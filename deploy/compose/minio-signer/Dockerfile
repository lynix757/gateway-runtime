FROM python:3.13-alpine
RUN pip install --no-cache-dir boto3==1.43.99
WORKDIR /app
COPY signer.py /app/signer.py
USER 65532:65532
EXPOSE 19002
ENTRYPOINT ["python", "/app/signer.py"]
