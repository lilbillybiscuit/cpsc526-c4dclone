FROM lilbillybiscuit/pytorch-c4d:latest

WORKDIR /app
COPY ./src .

RUN pip install -r requirements.txt

CMD ["python3", "train.py"]