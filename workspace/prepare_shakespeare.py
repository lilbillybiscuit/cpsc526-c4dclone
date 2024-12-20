import os
import requests
import numpy as np
import torch
import pickle
from tqdm import tqdm

def download_shakespeare():
    """Download Shakespeare dataset if it doesn't exist"""
    data_dir = os.path.join('data', 'shakespeare')
    os.makedirs(data_dir, exist_ok=True)
    
    # URL for Shakespeare's complete works
    url = 'https://raw.githubusercontent.com/karpathy/char-rnn/master/data/tinyshakespeare/input.txt'
    
    # Download the text file
    if not os.path.exists(os.path.join(data_dir, 'input.txt')):
        print(f'Downloading Shakespeare dataset...')
        response = requests.get(url)
        with open(os.path.join(data_dir, 'input.txt'), 'w', encoding='utf-8') as f:
            f.write(response.text)

def prepare_shakespeare():
    """Prepare the Shakespeare dataset for training"""
    data_dir = os.path.join('data', 'shakespeare')
    
    # First download the dataset if it doesn't exist
    download_shakespeare()
    
    # Read the text file
    with open(os.path.join(data_dir, 'input.txt'), 'r', encoding='utf-8') as f:
        text = f.read()
    
    print(f'Length of dataset in characters: {len(text):,}')
    
    # Get sorted list of unique characters
    chars = sorted(list(set(text)))
    vocab_size = len(chars)
    print(f'Vocabulary size (unique characters): {vocab_size:,}')
    
    # Create character to integer mapping
    stoi = {ch: i for i, ch in enumerate(chars)}
    itos = {i: ch for i, ch in enumerate(chars)}
    
    # Save the meta information
    meta = {
        'vocab_size': vocab_size,
        'itos': itos,
        'stoi': stoi,
    }
    with open(os.path.join(data_dir, 'meta.pkl'), 'wb') as f:
        pickle.dump(meta, f)
    
    print(f'Saved meta.pkl with vocabulary size {vocab_size}')
    
    # Encode the entire text dataset
    data = np.array([stoi[ch] for ch in text], dtype=np.uint16)
    
    # Create train-val split
    n = len(data)
    train_data = data[:int(n*0.9)]
    val_data = data[int(n*0.9):]
    
    # Save train and validation splits
    train_data.tofile(os.path.join(data_dir, 'train.bin'))
    val_data.tofile(os.path.join(data_dir, 'val.bin'))
    
    print(f'Saved train.bin ({len(train_data):,} tokens) and val.bin ({len(val_data):,} tokens)')

if __name__ == '__main__':
    prepare_shakespeare()
