FROM golang:1.22-alpine AS build
WORKDIR /app
COPY go.mod ./
COPY . .
RUN go mod tidy
RUN go build -o /bin/customer-access ./cmd

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=build /bin/customer-access /bin/customer-access
EXPOSE 8081
ENTRYPOINT ["/bin/customer-access"]