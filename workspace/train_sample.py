import os
import datetime
import time
import torch
import torch.nn as nn
from torch.nn.parallel import DistributedDataParallel as DDP
from torch.distributed import init_process_group, destroy_process_group
import torch.distributed as dist
import torch.nn.functional as F

os.environ['GLOO_SOCKET_IFNAME']='enp0s6'

class LargeTestModel(nn.Module):
    def __init__(self, input_size=1000, hidden_sizes=[2048, 1024, 2048, 512], output_size=100):
        super().__init__()
        self.layers = nn.ModuleList()
        
        # Input layer
        self.layers.append(nn.Linear(input_size, hidden_sizes[0]))
        
        # Hidden layers
        for i in range(len(hidden_sizes)-1):
            self.layers.append(nn.Linear(hidden_sizes[i], hidden_sizes[i+1]))
            
        # Output layer
        self.layers.append(nn.Linear(hidden_sizes[-1], output_size))
        
        # Add some conv layers to make it more compute-intensive
        self.convs = nn.ModuleList([
            nn.Conv1d(64, 64, kernel_size=3, padding=1),
            nn.Conv1d(64, 128, kernel_size=3, padding=1),
            nn.Conv1d(128, 64, kernel_size=3, padding=1)
        ])
        
        self.dropout = nn.Dropout(0.1)
        self.batch_norm = nn.BatchNorm1d(64)
        
        print(f"Created model with {self.count_parameters():,} parameters")
        
    def forward(self, x, y=None):
        # First part: Linear layers
        for i, layer in enumerate(self.layers[:-1]):
            x = layer(x)
            x = F.relu(x)
            x = self.dropout(x)
        
        # Reshape for conv layers
        batch_size = x.shape[0]
        x = x.view(batch_size, 64, -1)
        
        # Conv layers with residual connections
        residual = x
        for conv in self.convs:
            x = conv(x)
            x = F.relu(x)
        x = x + residual
        
        # Batch norm
        x = self.batch_norm(x)
        
        # Reshape back
        x = x.view(batch_size, -1)
        
        # Output layer
        x = self.layers[-1](x)
        
        # If training, compute loss
        if y is not None:
            loss = F.mse_loss(x, y)
            return x, loss
        return x, None
    
    def count_parameters(self):
        return sum(p.numel() for p in self.parameters() if p.requires_grad)

def setup_distributed():
    """Initialize distributed training with debugging"""
    if 'RANK' not in os.environ:
        return False, 0, 1, True
    
    print("Distributed Training Environment:")
    for key in ['RANK', 'LOCAL_RANK', 'WORLD_SIZE', 'MASTER_ADDR', 'MASTER_PORT']:
        print(f"{key}: {os.environ.get(key, 'NOT SET')}")
    
    try:
        init_process_group(
            backend='gloo',  # Use gloo for CPU
            init_method=f'tcp://{os.environ["MASTER_ADDR"]}:{os.environ["MASTER_PORT"]}',
            world_size=int(os.environ['WORLD_SIZE']),
            rank=int(os.environ['RANK']),
            timeout=datetime.timedelta(minutes=60),
        )
        dist.barrier()
        print(f"Process group initialized successfully on rank {os.environ['RANK']}")
    except Exception as e:
        print(f"Error initializing process group on rank {os.environ.get('RANK', 'unknown')}: {str(e)}")
        raise
    
    rank = int(os.environ['RANK'])
    local_rank = int(os.environ['LOCAL_RANK'])
    world_size = int(os.environ['WORLD_SIZE'])
    is_master = rank == 0
    
    return True, rank, world_size, is_master

def generate_batch(batch_size, input_size, output_size):
    """Generate random batch of data"""
    x = torch.randn(batch_size, input_size)
    y = torch.randn(batch_size, output_size)
    return x, y

def train():
    # Setup distributed training
    is_distributed, rank, world_size, is_master = setup_distributed()
    
    # Training settings
    input_size = 1000
    output_size = 100
    batch_size = 32 // world_size  # Global batch size divided by number of processes
    num_epochs = 10
    learning_rate = 0.001
    
    print(f"Process {rank}/{world_size} using batch size: {batch_size}")
    
    # Create model
    model = LargeTestModel(input_size=input_size, output_size=output_size)
    
    # Create optimizer before DDP wrapping
    optimizer = torch.optim.Adam(model.parameters(), lr=learning_rate)
    
    # Wrap model in DDP if distributed
    if is_distributed:
        model = DDP(model)
    
    # Training loop
    model.train()
    for epoch in range(num_epochs):
        for iter_num in range(100):  # 100 iterations per epoch
            # Generate random data
            x, y = generate_batch(batch_size, input_size, output_size)
            
            # Forward pass
            optimizer.zero_grad()
            _, loss = model(x, y)
            
            # Backward pass
            loss.backward()
            
            # Update weights
            optimizer.step()
            
            # Logging
            if is_master and iter_num % 10 == 0:
                print(f'Epoch {epoch}, Iter {iter_num}, Loss: {loss.item():.4f}')
    
    if is_distributed:
        destroy_process_group()

if __name__ == '__main__':
    train()
