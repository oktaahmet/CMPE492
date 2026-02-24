FROM golang:1.25-bookworm

WORKDIR /app

RUN apt-get update \
  && apt-get install -y --no-install-recommends emscripten \
  && rm -rf /var/lib/apt/lists/*

COPY app/go.mod app/go.sum ./
RUN go mod download

COPY app/ ./

EXPOSE 8080

CMD ["go", "run", "./cmd"]
