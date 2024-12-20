import os
import time
import math
import pickle
from contextlib import nullcontext
import multiprocessing

import numpy as np
import torch
from torch.nn.parallel import DistributedDataParallel as DDP
from torch.distributed import init_process_group, destroy_process_group

from model import GPTConfig, GPT

# -----------------------------------------------------------------------------
# Configuration
# -----------------------------------------------------------------------------
# Training
out_dir = 'out'
eval_interval = 20
log_interval = 1
eval_iters = 20
eval_only = False
always_save_checkpoint = True
init_from = 'scratch'

# Data
dataset = 'shakespeare'
batch_size = 32  # Global batch size, will be divided among processes
block_size = 64  # Smaller context window for CPU training
gradient_accumulation_steps = 1

# Model - Small configuration for testing
n_layer = 4
n_head = 4
n_embd = 128
dropout = 0.0
bias = True

# Optimizer
learning_rate = 1e-3
max_iters = 1000
weight_decay = 1e-1
beta1 = 0.9
beta2 = 0.95
grad_clip = 1.0

# Learning rate decay
decay_lr = True
warmup_iters = 100
lr_decay_iters = 1000
min_lr = 1e-4

# Distributed settings
backend = 'gloo'  # Use gloo backend for CPU training
n_processes = multiprocessing.cpu_count()  # Use all available CPU cores

def setup_distributed():
    """Initialize distributed training"""
    if 'RANK' not in os.environ:
        return False, 0, 1, True
    
    init_process_group(backend=backend)
    rank = int(os.environ['RANK'])
    local_rank = int(os.environ['LOCAL_RANK'])
    world_size = int(os.environ['WORLD_SIZE'])
    is_master = rank == 0
    
    # Set random seed based on rank for reproducibility
    torch.manual_seed(1337 + rank)
    
    return True, rank, world_size, is_master

def get_batch(data_dir, split, local_batch_size, block_size):
    """Load a batch of data"""
    filename = os.path.join(data_dir, f'{split}.bin')
    if not hasattr(get_batch, 'data_cache'):
        get_batch.data_cache = {}
    
    if filename not in get_batch.data_cache:
        data = np.memmap(filename, dtype=np.uint16, mode='r')
        get_batch.data_cache[filename] = data
    else:
        data = get_batch.data_cache[filename]
    
    ix = torch.randint(len(data) - block_size, (local_batch_size,))
    x = torch.stack([torch.from_numpy((data[i:i+block_size]).astype(np.int64)) for i in ix])
    y = torch.stack([torch.from_numpy((data[i+1:i+1+block_size]).astype(np.int64)) for i in ix])
    
    return x, y

def get_lr(iter_num):
    """Get learning rate based on current iteration"""
    if iter_num < warmup_iters:
        return learning_rate * iter_num / warmup_iters
    if iter_num > lr_decay_iters:
        return min_lr
    
    decay_ratio = (iter_num - warmup_iters) / (lr_decay_iters - warmup_iters)
    coeff = 0.5 * (1.0 + math.cos(math.pi * decay_ratio))
    return min_lr + coeff * (learning_rate - min_lr)
def train():
    # Setup distributed training
    is_distributed, rank, world_size, is_master = setup_distributed()
    
    # Calculate local batch size
    local_batch_size = batch_size // world_size
    print(f"Process {rank}/{world_size} using local batch size: {local_batch_size}")
    
    # Setup data directory
    data_dir = os.path.join('data', dataset)
    
    # Create output directory
    if is_master:
        os.makedirs(out_dir, exist_ok=True)
    
    # Load vocabulary size from meta.pkl if available
    meta_path = os.path.join(data_dir, 'meta.pkl')
    if os.path.exists(meta_path):
        with open(meta_path, 'rb') as f:
            meta = pickle.load(f)
            vocab_size = meta['vocab_size']
    else:
        vocab_size = 50304
    
    # Create model
    model_args = dict(
        n_layer=n_layer,
        n_head=n_head,
        n_embd=n_embd,
        block_size=block_size,
        bias=bias,
        vocab_size=vocab_size,
        dropout=dropout
    )
    
    model = GPT(GPTConfig(**model_args))
    
    # Create optimizer before DDP wrapping
    optimizer = model.configure_optimizers(
        weight_decay=weight_decay,
        learning_rate=learning_rate,
        betas=(beta1, beta2),
        device_type='cpu'
    )
    
    # Wrap model in DDP if distributed
    if is_distributed:
        model = DDP(model)
    
    # Training loop
    iter_num = 0
    best_val_loss = float('inf')
    
    while True:
        # Get current learning rate
        lr = get_lr(iter_num) if decay_lr else learning_rate
        for param_group in optimizer.param_groups:
            param_group['lr'] = lr
        
        # Get batch and train
        X, Y = get_batch(data_dir, 'train', local_batch_size, block_size)
        
        # Forward pass
        logits, loss = model(X, Y)
        
        # Backward pass
        loss.backward()
        
        # Gradient clipping
        if grad_clip != 0.0:
            torch.nn.utils.clip_grad_norm_(model.parameters(), grad_clip)
        
        # Update weights
        optimizer.step()
        optimizer.zero_grad(set_to_none=True)
        
        # Logging
        if is_master and iter_num % log_interval == 0:
            print(f'iter {iter_num}: loss {loss.item():.4f}, lr {lr:.2e}')
        
        # Evaluation
        if is_master and iter_num % eval_interval == 0:
            model.eval()
            with torch.no_grad():
                val_loss = 0.0
                for _ in range(eval_iters):
                    X, Y = get_batch(data_dir, 'val', local_batch_size, block_size)
                    _, loss = model(X, Y)
                    val_loss += loss.item()
                val_loss /= eval_iters
                
                print(f'iter {iter_num}: val_loss {val_loss:.4f}')
                
                # Save checkpoint if validation loss improved
                if val_loss < best_val_loss:
                    best_val_loss = val_loss
                    if iter_num > 0:
                        checkpoint = {
                            'model': model.module.state_dict() if is_distributed else model.state_dict(),
                            'optimizer': optimizer.state_dict(),
                            'model_args': model_args,
                            'iter_num': iter_num,
                            'best_val_loss': best_val_loss,
                        }
                        torch.save(checkpoint, os.path.join(out_dir, 'ckpt.pt'))
            
            model.train()
        
        iter_num += 1
        if iter_num > max_iters:
            break
    
    if is_distributed:
        destroy_process_group()

if __name__ == '__main__':
    # Run the training script
    print(f"Starting training with {n_processes} processes")
    train()
