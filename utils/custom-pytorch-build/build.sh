#!/bin/bash

docker build --build-arg CACHEBUST=$(date +%s) -t lilbillybiscuit/pytorch-c4d:latest .
docker push lilbillybiscuit/pytorch-c4d:latest
