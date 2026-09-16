FROM golang:1.22-alpine AS build
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN go build -o /bin/usage-webhooks ./cmd

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=build /bin/usage-webhooks /bin/usage-webhooks
EXPOSE 8083
ENTRYPOINT ["/bin/usage-webhooks"]
