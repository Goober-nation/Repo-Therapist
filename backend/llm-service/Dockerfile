FROM python:3.11-slim
WORKDIR /app
COPY pyproject.toml ./
COPY src/ src/
COPY config/ config/
RUN pip install --no-cache-dir . && useradd -u 10001 app
USER app
CMD ["python", "-m", "llm_service", "--listen"]
