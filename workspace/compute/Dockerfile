FROM lilbillybiscuit/pytorch-cpu:latest

WORKDIR /app
COPY . .

RUN pip install -r requirements.txt

CMD ["python3", "train.py"]