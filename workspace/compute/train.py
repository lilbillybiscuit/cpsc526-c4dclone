import os
import time
import math
import pickle
from contextlib import nullcontext

import numpy as np
import torch
from torch.nn.parallel import DistributedDataParallel as DDP
from torch.distributed import init_process_group, destroy_process_group

from model import GPTConfig, GPT
# os.environ["GLOO_SOCKET_IFNAME"]="enp0s6" 

# -----------------------------------------------------------------------------
# default config values for a small model suitable for CPU training
out_dir = 'out'
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
# -----------------------------------------------------------------------------

def setup_distributed():
    if 'RANK' not in os.environ:
        return False, 0, 1, True
    
    # Initialize distributed training
    init_process_group(backend=backend)
    rank = int(os.environ['RANK'])
    local_rank = int(os.environ['LOCAL_RANK'])
    world_size = int(os.environ['WORLD_SIZE'])
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
    is_distributed, rank, world_size, is_master = setup_distributed()
    
    # Adjust batch size and gradient accumulation for distributed training
    global batch_size, gradient_accumulation_steps
    if is_distributed:
        batch_size = max(batch_size // world_size, 1)
        gradient_accumulation_steps = max(gradient_accumulation_steps // world_size, 1)
    
    data_dir = os.path.join('data', dataset)
    
    if is_master:
        os.makedirs(out_dir, exist_ok=True)
    
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
    
    iter_num = 0
    best_val_loss = float('inf')
    
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
                        torch.save(checkpoint, os.path.join(out_dir, 'ckpt.pt'))
            
            model.train()
        
        iter_num += 1
        if iter_num > max_iters:
            break
    
    if is_distributed:
        destroy_process_group()

if __name__ == '__main__':
    train()
