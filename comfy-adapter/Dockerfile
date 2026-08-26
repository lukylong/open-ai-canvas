FROM python:3.12-slim

WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY app.py workflow.py ./
COPY workflows ./workflows

EXPOSE 8090
HEALTHCHECK --interval=20s --timeout=5s --retries=3 CMD python -c "import urllib.request; urllib.request.urlopen('http://127.0.0.1:8090/health').read()"
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8090"]
