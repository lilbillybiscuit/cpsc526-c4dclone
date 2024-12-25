import os
import time
import math
import pickle
from contextlib import nullcontext
import signal

import numpy as np
import torch
from torch.nn.parallel import DistributedDataParallel as DDP
from torch.distributed import init_process_group, destroy_process_group

from recovery import C4DRecovery

from model import GPTConfig, GPT
# os.environ["GLOO_SOCKET_IFNAME"]="enp0s6"

# Available variables from PyTorchOperator
# MASTER_ADDR, MASTER_PORT, PET_MASTER_ADDR, PET_MASTER_PORT, PET_NNODES, PET_NODE_RANK, PET_NPROC_PER_NODE, RANK, WORLD_SIZE, TASK_ID
CHECKPOINT_DIR = os.environ.get("CHECKPOINT_DIR", "/mnt/checkpoints")

# -----------------------------------------------------------------------------
# default config values for a small model suitable for CPU training
eval_interval = 20
log_interval = 1
eval_iters = 20
eval_only = False
always_save_checkpoint = True
init_from = 'scratch'

# data
dataset = 'shakespeare'  # Using Shakespeare dataset as it's smaller
gradient_accumulation_steps = 1
batch_size = 8  # Smaller batch size for CPU
block_size = 64  # Smaller context window

# model - smaller configuration
n_layer = 4
n_head = 4
n_embd = 128
dropout = 0.0
bias = True

# optimizer
learning_rate = 3e-4  # Reduced learning rate
max_iters = 1000
weight_decay = 0.1

beta1 = 0.9
beta2 = 0.95
grad_clip = 0.5  # Reduced gradient clipping

# learning rate decay
decay_lr = True
warmup_iters = 100
lr_decay_iters = 1000
min_lr = 1e-4

# distributed training
backend = 'gloo'  # Use gloo backend for CPU training

# recovery instance
recovery = C4DRecovery(f"http://{os.environ.get('TASK_ID')}.{os.environ.get('NAMESPACE')}:8081")
# -----------------------------------------------------------------------------

def update_env(signal_number, frame):
    # Fetch updated environment variables dynamically
    env_vars = recovery.fetch_env_vars()
    os.environ.update(env_vars)
    print("Environment variables updated:", env_vars)

signal.signal(signal.SIGUSR1, update_env)

def handle_offload_task(signal_number, frame):
    print("Received offload request. Loading latest checkpoint...")
    # Load the latest checkpoint for the current rank
    checkpoint_path = os.path.join(CHECKPOINT_DIR, f'ckpt_rank{rank}.pt')
    if os.path.isfile(checkpoint_path):
        checkpoint = torch.load(checkpoint_path, map_location='cpu')
        model.load_state_dict(checkpoint['model'])
        optimizer.load_state_dict(checkpoint['optimizer'])
        start_iter = checkpoint.get('iter_num', 0) + 1
        print(f"Resumed training from checkpoint at iteration {start_iter}")
    else:
        print("No checkpoint found for this rank. Starting fresh.")

# offload signal handler
signal.signal(signal.SIGUSR2, handle_offload_task)

def setup_distributed():
    # Initialize distributed training
    init_process_group(backend=backend)
    
    env_vars = recovery.fetch_env_vars()
    rank = int(env_vars.get('RANK', -1))
    world_size = int(env_vars.get('WORLD_SIZE', 1))
    if rank == -1:
        return False, 0, 1, True
    
    is_master = rank == 0
    
    # Set random seed based on rank
    torch.manual_seed(1337 + rank)
    
    return True, rank, world_size, is_master

def get_batch(data_dir, split, batch_size, block_size):
    # Load data
    filename = os.path.join(data_dir, f'{split}.bin')
    if not hasattr(get_batch, 'data_cache'):
        get_batch.data_cache = {}
    
    if filename not in get_batch.data_cache:
        data = np.memmap(filename, dtype=np.uint16, mode='r')
        get_batch.data_cache[filename] = data
    else:
        data = get_batch.data_cache[filename]
    
    ix = torch.randint(len(data) - block_size, (batch_size,))
    x = torch.stack([torch.from_numpy((data[i:i+block_size]).astype(np.int64)) for i in ix])
    y = torch.stack([torch.from_numpy((data[i+1:i+1+block_size]).astype(np.int64)) for i in ix])
    
    return x, y
def get_lr(iter_num):
    # Linear warmup followed by cosine decay
    if iter_num < warmup_iters:
        return learning_rate * iter_num / warmup_iters
    decay_ratio = (iter_num - warmup_iters) / (max_iters - warmup_iters)
    coeff = 0.5 * (1.0 + math.cos(math.pi * decay_ratio))
    return min_lr + coeff * (learning_rate - min_lr)

def train():
    print("Starting training process...")
    is_distributed, rank, world_size, is_master = setup_distributed()
    
    # Adjust batch size and gradient accumulation for distributed training
    global batch_size, gradient_accumulation_steps
    if is_distributed:
        batch_size = max(batch_size // world_size, 1)
        gradient_accumulation_steps = max(gradient_accumulation_steps // world_size, 1)
    
    data_dir = os.path.join('data', dataset)
    
    if is_master:
        os.makedirs(CHECKPOINT_DIR, exist_ok=True)
        os.makedirs(CHECKPOINT_DIR, exist_ok=True)
        print(f"Checkpoints will be saved to: {CHECKPOINT_DIR}")
    
    # Model initialization
    if os.path.exists(os.path.join(data_dir, 'meta.pkl')):
        with open(os.path.join(data_dir, 'meta.pkl'), 'rb') as f:
            meta = pickle.load(f)
            vocab_size = meta['vocab_size']
    else:
        vocab_size = 50304
    
    model_args = dict(
        n_layer=n_layer,
        n_head=n_head,
        n_embd=n_embd,
        block_size=block_size,
        bias=bias,
        vocab_size=vocab_size,
        dropout=dropout
    )
    
    # Create model with careful initialization
    gpt_model = GPT(GPTConfig(**model_args))
    
    # Initialize weights carefully
    def _init_weights(module):
        if isinstance(module, (torch.nn.Linear, torch.nn.Embedding)):
            torch.nn.init.normal_(module.weight, mean=0.0, std=0.02)
            if isinstance(module, torch.nn.Linear) and module.bias is not None:
                torch.nn.init.zeros_(module.bias)
    
    gpt_model.apply(_init_weights)
    
    
    # Wrap model in DDP
    if is_distributed:
        model = DDP(gpt_model)
    else:
        model = gpt_model
    
    # Create optimizer with better defaults
    optimizer = (gpt_model if not is_distributed else model.module).configure_optimizers(
        weight_decay=weight_decay,
        learning_rate=learning_rate,
        betas=(beta1, beta2),
        device_type='cpu'
    )

    start_iter = 0
    # if os.path.isfile(os.path.join(CHECKPOINT_DIR, 'ckpt.pt')):
    #     checkpoint = torch.load(os.path.join(CHECKPOINT_DIR, 'ckpt.pt'), map_location='cpu')
    #     # Load model and optimizer state
    #     gpt_model.load_state_dict(checkpoint['model'])
    #     optimizer.load_state_dict(checkpoint['optimizer'])
    #     # Load other variables
    #     start_iter = checkpoint.get('iter_num', 0) + 1  # Start from the next iteration
    #     best_val_loss = checkpoint.get('best_val_loss', float('inf'))
    #     print(f"Resuming training from iteration {start_iter}")
    
    iter_num = start_iter
    best_val_loss = float('inf')
    
    start_time = time.time()
    while True:
        # Adjust learning rate
        lr = get_lr(iter_num)
        for param_group in optimizer.param_groups:
            param_group['lr'] = lr
        
        # Get batch
        X, Y = get_batch(data_dir, 'train', batch_size, block_size)
        
        # Forward pass
        logits, loss = model(X, Y)
        
        loss = loss / gradient_accumulation_steps
        
        # Backward pass
        loss.backward()
        
        # Gradient clipping
        if grad_clip != 0.0:
            torch.nn.utils.clip_grad_norm_(model.parameters(), grad_clip)
        
        # Check for NaN gradients
        valid_gradients = True
        for param in model.parameters():
            if param.grad is not None:
                if torch.isnan(param.grad).any():
                    valid_gradients = False
                    break
        
        if not valid_gradients:
            print(f"NaN gradients encountered at iteration {iter_num}. Skipping update.")
            optimizer.zero_grad(set_to_none=True)
            continue
        
        # Update weights
        optimizer.step()
        optimizer.zero_grad(set_to_none=True)
        
        # Logging
        if is_master and iter_num % log_interval == 0:
            print(f'iter {iter_num}: loss {loss.item()*gradient_accumulation_steps:.4f}, lr {lr:.2e}')
        
        # Evaluation
        if is_master and iter_num % eval_interval == 0:
            model.eval()
            with torch.no_grad():
                val_loss = 0.0
                for _ in range(eval_iters):
                    X, Y = get_batch(data_dir, 'val', batch_size, block_size)
                    _, loss = model(X, Y)
                    val_loss += loss.item()
                val_loss /= eval_iters
                
                print(f'iter {iter_num}: val_loss {val_loss:.4f}')
                
                if val_loss < best_val_loss:
                    best_val_loss = val_loss
                    if iter_num > 0:
                        checkpoint = {
                            'model': model.state_dict(),
                            'optimizer': optimizer.state_dict(),
                            'model_args': model_args,
                            'iter_num': iter_num,
                            'best_val_loss': best_val_loss,
                        }
                        torch.save(checkpoint, os.path.join(CHECKPOINT_DIR, 'ckpt.pt'))
                        recovery.report_event(
                            event_type="training_time", 
                            details={"iteration_time": time.time() - start_time, "iteration": iter_num}
                        )
                        start_time = time.time()
                        print(f"Checkpoint saved at iteration {iter_num}")
            
            model.train()
        
        iter_num += 1
        if iter_num > max_iters:
            break
    
    if is_distributed:
        destroy_process_group()

if __name__ == '__main__':
    # print out all environment variables and configuration
#     import pprint
#     pprint.pprint(dict(os.environ))
    train()
