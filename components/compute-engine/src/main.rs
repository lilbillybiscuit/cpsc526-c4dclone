use actix_web::{web, App, HttpResponse, HttpServer, Responder};
use serde::{Serialize, Deserialize};
use std::sync::Mutex;
use tokio::time::sleep;
use tokio::process::Command as TokioCommand;
use tokio::process::Child as TokioChild;
use env_logger;

use tokio::time::{timeout, Duration};
use signal_hook::consts::signal::{SIGTERM, SIGINT, SIGKILL};
use std::env;
use std::os::unix::process::ExitStatusExt;
use std::time::{SystemTime, UNIX_EPOCH};
use sysinfo::System;


#[derive(Clone, Serialize)]
struct ExecutionInfo {
    start_time: u64,
    end_time: Option<u64>,
    exit_code: Option<i32>,
    exit_signal: Option<i32>,
    pid: Option<u32>,
    env_vars: Option<std::collections::HashMap<String, String>>,
    script_path: String,
}

#[derive(Deserialize, Clone, Debug)]
struct ScriptParams {
    env_vars: Option<std::collections::HashMap<String, String>>,
    script_path: Option<String>,  // Optional override for the script path
}


struct AppState {
    script_process: Mutex<Option<TokioChild>>,
    last_execution: Mutex<Option<ExecutionInfo>>,
}


#[derive(Serialize)]
struct StatusResponse {
    current_status: String,
    current_pid: Option<u32>,
    last_execution: Option<ExecutionInfo>,
}

fn get_timestamp() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap()
        .as_secs()
}

#[derive(Serialize)]
struct MetricsResponse {
    cpu_percent: f32,
    memory_kb: u64,
    status: String,
}

#[derive(Serialize)]
struct ErrorResponse {
    error: String,
}
async fn start_script(
    data: web::Data<AppState>,
    params: web::Json<ScriptParams>,
) -> impl Responder {
    let mut process_guard = data.script_process.lock().unwrap();

    if let Some(child) = &mut *process_guard {
        match child.try_wait() {
            Ok(Some(status)) => {
                *process_guard = None;
            }
            Ok(None) => {
                return HttpResponse::BadRequest().json(ErrorResponse {
                    error: "A process is already running. Stop it first.".to_string(),
                });
            }
            Err(_) => {
                *process_guard = None;
            }
        }
    }

    let script_path = params.script_path.as_deref().unwrap_or("launch.sh");
    let mut cmd = TokioCommand::new("/bin/sh");
    cmd.arg(script_path);

    if let Some(env_vars) = &params.env_vars {
        for (key, value) in env_vars {
            cmd.env(key, value);
        }
    }

    match cmd.spawn() {
        Ok(mut child) => {
            let pid = child.id();

            // Create new execution info
            let exec_info = ExecutionInfo {
                start_time: get_timestamp(),
                end_time: None,
                exit_code: None,
                exit_signal: None,
                pid: pid,
                env_vars: params.env_vars.clone(),
                script_path: script_path.to_string(),
            };

            match child.try_wait() {
                Ok(Some(status)) => {

                    let mut final_exec_info = exec_info;
                    final_exec_info.end_time = Some(get_timestamp());
                    final_exec_info.exit_code = status.code();
                    final_exec_info.exit_signal = status.signal();

                    let mut last_execution = data.last_execution.lock().unwrap();
                    *last_execution = Some(final_exec_info.clone());

                    HttpResponse::Ok().json(StatusResponse {
                        current_status: "exited".to_string(),
                        current_pid: None,
                        last_execution: Some(final_exec_info),
                    })
                }
                Ok(None) => {

                    *process_guard = Some(child);
                    let mut last_execution = data.last_execution.lock().unwrap();
                    *last_execution = Some(exec_info.clone());

                    HttpResponse::Ok().json(StatusResponse {
                        current_status: "started".to_string(),
                        current_pid: pid,
                        last_execution: Some(exec_info),
                    })
                }
                Err(e) => HttpResponse::InternalServerError().json(ErrorResponse {
                    error: format!("Error checking process status: {}", e),
                })
            }
        }
        Err(e) => HttpResponse::InternalServerError().json(ErrorResponse {
            error: format!("Failed to start process: {}", e),
        }),
    }
}

async fn kill_process_tree(pid: u32) {

    let _ = TokioCommand::new("pkill")
        .arg("-P")
        .arg(pid.to_string())
        .output()
        .await;


    unsafe {
        libc::kill(pid as i32, SIGKILL);
    }
}
async fn stop_script(data: web::Data<AppState>) -> impl Responder {
    let mut process_guard = data.script_process.lock().unwrap();
    let mut last_execution = data.last_execution.lock().unwrap();
    println!("Stopping process");

    if let Some(mut child) = process_guard.take() {
        if let Some(pid) = child.id() {
            println!("Sending SIGKILL to {}", pid);
            kill_process_tree(pid).await;


            match timeout(Duration::from_secs(1), child.wait()).await {
                Ok(Ok(status)) => {

                    if let Some(exec_info) = last_execution.as_mut() {
                        exec_info.end_time = Some(get_timestamp());
                        exec_info.exit_code = status.code();
                        exec_info.exit_signal = status.signal();
                    }

                    HttpResponse::Ok().json(StatusResponse {
                        current_status: "stopped".to_string(),
                        current_pid: None,
                        last_execution: last_execution.clone(),
                    })
                }
                Ok(Err(e)) => {
                    HttpResponse::InternalServerError().json(ErrorResponse {
                        error: format!("Error waiting for process: {}", e),
                    })
                }
                Err(_) => {

                    if let Some(exec_info) = last_execution.as_mut() {
                        exec_info.end_time = Some(get_timestamp());
                        exec_info.exit_signal = Some(SIGKILL);
                    }

                    HttpResponse::Ok().json(StatusResponse {
                        current_status: "force_stopped".to_string(),
                        current_pid: None,
                        last_execution: last_execution.clone(),
                    })
                }
            }
        } else {
            HttpResponse::InternalServerError().json(ErrorResponse {
                error: "No PID available for process".to_string(),
            })
        }
    } else {
        HttpResponse::BadRequest().json(ErrorResponse {
            error: "No process running".to_string(),
        })
    }
}

async fn get_status(data: web::Data<AppState>) -> impl Responder {
    let mut process_guard = data.script_process.lock().unwrap();
    let mut last_execution = data.last_execution.lock().unwrap();

    if let Some(child) = &mut *process_guard {
        match child.try_wait() {
            Ok(Some(status)) => {

                if let Some(exec_info) = last_execution.as_mut() {
                    exec_info.end_time = Some(get_timestamp());
                    exec_info.exit_code = status.code();
                    exec_info.exit_signal = status.signal();
                }


                *process_guard = None;

                HttpResponse::Ok().json(StatusResponse {
                    current_status: "exited".to_string(),
                    current_pid: None,
                    last_execution: last_execution.clone(),
                })
            }
            Ok(None) => {

                HttpResponse::Ok().json(StatusResponse {
                    current_status: "running".to_string(),
                    current_pid: child.id(),
                    last_execution: last_execution.clone(),
                })
            }
            Err(e) => {

                *process_guard = None;
                HttpResponse::Ok().json(StatusResponse {
                    current_status: format!("error: {}", e),
                    current_pid: None,
                    last_execution: last_execution.clone(),
                })
            }
        }
    } else {
        HttpResponse::Ok().json(StatusResponse {
            current_status: "not_running".to_string(),
            current_pid: None,
            last_execution: last_execution.clone(),
        })
    }
}


#[derive(Serialize)]
struct SystemMetrics {
    // System-wide metrics
    system_cpu_usage: f32,
    system_memory_total_bytes: u64,
    system_memory_used_bytes: u64,
    system_memory_free_bytes: u64,
    system_memory_available_bytes: u64,
    system_swap_total_bytes: u64,
    system_swap_used_bytes: u64,
    system_swap_free_bytes: u64,
    system_load_average_1m: f64,
    system_load_average_5m: f64,
    system_load_average_15m: f64,
    system_uptime_secs: u64,
    total_processes: usize,
    cpu_cores: usize,
    cpu_threads: usize,

    // Process-specific metrics (optional)
    process_metrics: Option<ProcessMetrics>,
}

#[derive(Serialize)]
struct ProcessMetrics {
    pid: u32,
    cpu_usage: f32,
    memory_bytes: u64,
    virtual_memory_bytes: u64,
    run_time_secs: u64,
    disk_read_bytes: u64,
    disk_written_bytes: u64,
    status: String,
    parent_pid: Option<u32>,
}

async fn get_metrics(data: web::Data<AppState>) -> impl Responder {
    let process_guard = data.script_process.lock().unwrap();
    let mut system = System::new_all();


    system.refresh_all();


    let mut metrics = SystemMetrics {
        system_cpu_usage: system.global_cpu_usage(),
        system_memory_total_bytes: system.total_memory(),
        system_memory_used_bytes: system.used_memory(),
        system_memory_free_bytes: system.free_memory(),
        system_memory_available_bytes: system.available_memory(),
        system_swap_total_bytes: system.total_swap(),
        system_swap_used_bytes: system.used_swap(),
        system_swap_free_bytes: system.free_swap(),
        system_load_average_1m: System::load_average().one,
        system_load_average_5m: System::load_average().five,
        system_load_average_15m: System::load_average().fifteen,
        system_uptime_secs: System::uptime(),
        total_processes: system.processes().len(),
        cpu_cores: system.physical_core_count().unwrap_or(0),
        cpu_threads: system.cpus().len(),
        process_metrics: None,
    };

    if let Some(child) = &*process_guard {
        if let Some(pid) = child.id() {
            let sys_pid = sysinfo::Pid::from_u32(pid);

            if let Some(process) = system.process(sys_pid) {
                let status_str = match process.status() {
                    sysinfo::ProcessStatus::Run => "running",
                    sysinfo::ProcessStatus::Sleep => "sleeping",
                    sysinfo::ProcessStatus::Stop => "stopped",
                    sysinfo::ProcessStatus::Zombie => "zombie",
                    sysinfo::ProcessStatus::Idle => "idle",
                    _ => "unknown",
                };

                metrics.process_metrics = Some(ProcessMetrics {
                    pid,
                    cpu_usage: process.cpu_usage(),
                    memory_bytes: process.memory(),
                    virtual_memory_bytes: process.virtual_memory(),
                    run_time_secs: process.run_time(),
                    disk_read_bytes: process.disk_usage().read_bytes,
                    disk_written_bytes: process.disk_usage().written_bytes,
                    status: status_str.to_string(),
                    parent_pid: process.parent().map(|p| p.as_u32()),
                });
            }
        }
    }

    let memory_usage_percent = (metrics.system_memory_used_bytes as f64 / metrics.system_memory_total_bytes as f64 * 100.0) as f32;
    let swap_usage_percent = (metrics.system_swap_used_bytes as f64 / metrics.system_swap_total_bytes as f64 * 100.0) as f32;

    HttpResponse::Ok().json(serde_json::json!({
        "timestamp": SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap()
            .as_secs(),
        "metrics": metrics,
        "derived_metrics": {
            "memory_usage_percent": memory_usage_percent,
            "swap_usage_percent": swap_usage_percent,
            "memory_available_percent": 100.0 - memory_usage_percent,
        }
    }))
}



async fn stop_script_advanced(data: web::Data<AppState>) -> impl Responder {
    let mut process_guard = data.script_process.lock().unwrap();

    if let Some(mut child) = process_guard.take() {
        let pid = child.id().unwrap_or(0) as i32;
        let start_time = std::time::Instant::now();

        // Try SIGTERM first
        unsafe { libc::kill(pid, SIGTERM) };

        match timeout(Duration::from_secs(2), child.wait()).await {
            Ok(Ok(_)) => {
                return HttpResponse::Ok().json(serde_json::json!({
                    "status": "stopped",
                    "shutdown_type": "graceful",
                    "duration_ms": start_time.elapsed().as_millis(),
                }));
            }
            _ => {
                // Try SIGINT
                unsafe { libc::kill(pid, SIGINT) };

                match timeout(Duration::from_secs(1), child.wait()).await {
                    Ok(Ok(_)) => {
                        return HttpResponse::Ok().json(serde_json::json!({
                            "status": "stopped",
                            "shutdown_type": "interrupt",
                            "duration_ms": start_time.elapsed().as_millis(),
                        }));
                    }
                    _ => {
                        // Finally, SIGKILL
                        unsafe { libc::kill(pid, SIGKILL) };

                        match timeout(Duration::from_secs(1), child.wait()).await {
                            Ok(Ok(_)) => {
                                HttpResponse::Ok().json(serde_json::json!({
                                    "status": "stopped",
                                    "shutdown_type": "force_kill",
                                    "duration_ms": start_time.elapsed().as_millis(),
                                }))
                            }
                            _ => {
                                HttpResponse::InternalServerError().json(ErrorResponse {
                                    error: "Failed to kill process even with SIGKILL".to_string(),
                                })
                            }
                        }
                    }
                }
            }
        }
    } else {
        HttpResponse::BadRequest().json(ErrorResponse {
            error: "No process running".to_string(),
        })
    }
}

async fn get_env(data: web::Data<AppState>) -> impl Responder {
    let process_guard = data.script_process.lock().unwrap();
    if let Some(child) = &*process_guard {
        let env_vars = child.env().unwrap_or_default();
        HttpResponse::Ok().json(env_vars)
    } else {
        HttpResponse::BadRequest().json(ErrorResponse {
            error: "No process running".to_string(),
        })
    }
}

async fn restart_script(
    data: web::Data<AppState>,
    params: web::Json<ScriptParams>,
) -> impl Responder {
    let stop_result = stop_script(data.clone()).await;
    sleep(Duration::from_secs(2)).await;
    start_script(data, params).await
}
#[actix_web::main]
async fn main() -> std::io::Result<()> {
    let app_state = web::Data::new(AppState {
        script_process: Mutex::new(None),
        last_execution: Mutex::new(None),
    });


    let port = env::var("AGENT_PORT").unwrap_or_else(|_| "8090".to_string());
    println!("Starting server at http://0.0.0.0:{}", port);
    let app_state_clone = app_state.clone();
    let server = HttpServer::new(move || {
        App::new()
            .app_data(app_state.clone())
            .route("/start", web::post().to(start_script))
            .route("/stop", web::post().to(stop_script))
            .route("/status", web::get().to(get_status))
            .route("/metrics", web::get().to(get_metrics))
            .route("/restart", web::post().to(restart_script))
            .route("/env", web::get().to(get_env))
    })
        .bind(format!("0.0.0.0:{}", port))?
        .run();

    // start_script(app_state_clone, web::Json(ScriptParams {
    //     env_vars: None,
    //     script_path: None
    // }))
    //     .await;

    server.await
}
