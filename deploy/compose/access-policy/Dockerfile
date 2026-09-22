FROM python:3.13-alpine
WORKDIR /app
COPY policy.py /app/policy.py
USER 65532:65532
EXPOSE 19100
ENTRYPOINT ["python", "/app/policy.py"]
