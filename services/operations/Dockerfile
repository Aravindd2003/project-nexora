FROM golang:1.22-alpine AS build
WORKDIR /app
COPY go.mod ./
COPY . .
RUN go mod tidy
RUN go build -o /bin/operations ./cmd

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=build /bin/operations /bin/operations
EXPOSE 8082
ENTRYPOINT ["/bin/operations"]