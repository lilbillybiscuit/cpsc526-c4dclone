use actix_web::{web, App, HttpResponse, HttpServer, Responder};
use serde::{Serialize};
use std::sync::Mutex;
use tokio::time::sleep;
use tokio::process::Command as TokioCommand;
use tokio::process::Child as TokioChild;
use env_logger;

use tokio::time::{timeout, Duration};
use signal_hook::consts::signal::{SIGTERM, SIGINT, SIGKILL};
use std::env;
use sysinfo::System;

// Structure to hold the process state
struct AppState {
    script_process: Mutex<Option<TokioChild>>,
}

// Response structures
#[derive(Serialize)]
struct StatusResponse {
    status: String,
    pid: Option<u32>,
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

// Start the script
async fn start_script(data: web::Data<AppState>) -> impl Responder {

    match TokioCommand::new("/bin/sh").arg("launch.sh").spawn() {
        Ok(child) => {
            let pid = child.id();
            let mut process_guard = data.script_process.lock().unwrap();
            *process_guard = Some(child);

            HttpResponse::Ok().json(StatusResponse {
                status: "started".to_string(),
                pid: Some(pid.unwrap()),
            })
        }
        Err(e) => HttpResponse::InternalServerError().json(ErrorResponse {
            error: format!("Failed to start process: {}", e),
        }),
    }
}
async fn stop_script(data: web::Data<AppState>) -> impl Responder {
    let mut process_guard = data.script_process.lock().unwrap();

    if let Some(mut child) = process_guard.take() {
        // First try graceful shutdown
        if let Err(e) = child.kill().await {
            return HttpResponse::InternalServerError().json(ErrorResponse {
                error: format!("Failed to send kill signal: {}", e),
            });
        }

        // Wait for up to 30 seconds for the process to exit gracefully
        match timeout(Duration::from_secs(30), child.wait()).await {
            Ok(wait_result) => {
                match wait_result {
                    Ok(_) => HttpResponse::Ok().json(StatusResponse {
                        status: "stopped".to_string(),
                        pid: None,
                    }),
                    Err(e) => HttpResponse::InternalServerError().json(ErrorResponse {
                        error: format!("Error waiting for process to stop: {}", e),
                    }),
                }
            },
            Err(_) => {
                println!("Process didn't stop gracefully, forcing kill...");

                unsafe {
                    libc::kill(child.id().unwrap() as i32, SIGKILL);
                }

                // Give it a short time to die
                match timeout(Duration::from_secs(5), child.wait()).await {
                    Ok(_) => HttpResponse::Ok().json(StatusResponse {
                        status: "force_stopped".to_string(),
                        pid: None,
                    }),
                    Err(_) => {
                        HttpResponse::InternalServerError().json(ErrorResponse {
                            error: "Failed to kill process even with SIGKILL".to_string(),
                        })
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

async fn get_status(data: web::Data<AppState>) -> impl Responder {
    let process_guard = data.script_process.lock().unwrap();

    if let Some(child) = &*process_guard {
        match child.id() {
            Some(pid) => HttpResponse::Ok().json(StatusResponse {
                status: "running".to_string(),
                pid: Some(pid),
            }),
            None => HttpResponse::Ok().json(StatusResponse {
                status: "finished".to_string(),
                pid: None,
            }),
        }
    } else {
        HttpResponse::Ok().json(StatusResponse {
            status: "not_running".to_string(),
            pid: None,
        })
    }
}

async fn get_metrics(data: web::Data<AppState>) -> impl Responder {
    let process_guard = data.script_process.lock().unwrap();

    if let Some(child) = &*process_guard {
        if let Some(pid) = child.id() {
            let mut system = System::new_all();
            system.refresh_all();

            // Convert pid to sysinfo::Pid
            let sys_pid = sysinfo::Pid::from_u32(pid);

            if let Some(process) = system.process(sys_pid) {
                HttpResponse::Ok().json(serde_json::json!({
                    "status": "running",
                    "pid": pid,
                    "cpu_usage": process.cpu_usage(),
                    "memory_bytes": process.memory(),
                    "virtual_memory_bytes": process.virtual_memory(),
                    "run_time": process.run_time()
                }))
            } else {
                HttpResponse::NotFound().json(ErrorResponse {
                    error: "Process not found in system metrics".to_string(),
                })
            }
        } else {
            HttpResponse::NotFound().json(ErrorResponse {
                error: "Process ID not available".to_string(),
            })
        }
    } else {
        HttpResponse::BadRequest().json(ErrorResponse {
            error: "No process running".to_string(),
        })
    }
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
// Restart the script
async fn restart_script(data: web::Data<AppState>) -> impl Responder {
    let stop_result = stop_script(data.clone()).await;
    sleep(Duration::from_secs(2)).await;
    start_script(data).await
}

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    let app_state = web::Data::new(AppState {
        script_process: Mutex::new(None),
    });

    match TokioCommand::new("/bin/sh").arg("launch.sh").spawn() {
        Ok(child) => {
            let pid = child.id();
            println!("Started initial process with PID: {:?}", pid);
            let mut process_guard = app_state.script_process.lock().unwrap();
            *process_guard = Some(child);
        }
        Err(e) => {
            panic!("Failed to start initial process: {}", e);
        }
    };

    println!("Starting server at http://0.0.0.0:8080");
    let port = env::var("PORT").unwrap_or_else(|_| "8080".to_string());

    HttpServer::new(move || {
        App::new()
            .app_data(app_state.clone())
            .route("/start", web::post().to(start_script))
            .route("/stop", web::post().to(stop_script))
            .route("/status", web::get().to(get_status))
            .route("/metrics", web::get().to(get_metrics))
            .route("/restart", web::post().to(restart_script))
    })
        .bind(format!("0.0.0.0:{}", port))?
        .run()
        .await
}
