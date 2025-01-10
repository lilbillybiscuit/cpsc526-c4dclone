import time
import os
import argparse
# print all environment variables
print(os.environ["RANK"])
print(os.environ["WORLD_SIZE"])
print(os.environ["MASTER_ADDR"])
print(os.environ["MASTER_PORT"])
cnt = 0

# Parse arguments
stf = os.environ.get('STF', 10)

while True:
    print("Hello, this message appears every second!")
    time.sleep(1)  # Pauses execution for 1 second
    cnt +=1
    if cnt > int(stf):
        raise Exception("This is an exception")

