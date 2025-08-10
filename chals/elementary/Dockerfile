FROM python:3.12 AS app

RUN apt update && apt install -y socat

WORKDIR /app
COPY main.py ./

CMD ["socat" , "TCP-LISTEN:5000,reuseaddr,fork", "EXEC:python3 main.py"]
