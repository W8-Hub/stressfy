FROM golang:1.23-alpine AS build

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /stressfy ./cmd/stressfy

FROM gcr.io/distroless/static-debian12 AS runner

ENV PORT=3333
ENV DATA_DIR=/tmp/stress-api
ENV TZ_OFFSET=-03:00

COPY --from=build /stressfy /stressfy

EXPOSE 3333

ENTRYPOINT ["/stressfy"]
