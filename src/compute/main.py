import os
import torch
import torch.distributed as dist
from model import GPT
from train import train

def main():
    # Initialize distributed training
    dist.init_process_group(backend='gloo')
    
    # Start training
    train()

if __name__ == '__main__':
    main()