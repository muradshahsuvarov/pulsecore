# Use the official Golang image to create a build artifact.
FROM golang:1.20 AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy everything from the current directory to the Working Directory inside the container
COPY . .

# Resolve dependencies discrepancies
RUN go mod tidy -compat=1.20

# Download dependencies
RUN go mod download

# Build the server binary
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./services/matchmaking/src/main.go

# Use the Alpine-based image as it's smaller than the default golang image
FROM alpine:latest

# Install CA certificates
RUN apk --no-cache add ca-certificates

# Set the Current Working Directory inside the container
WORKDIR /root/

# Copy the server binary from the builder stage
COPY --from=builder /app/main .

# Use shell form for CMD to interpolate environment variables
CMD ./main -server $SERVER_ADDR -redis-server $REDIS_ADDR
