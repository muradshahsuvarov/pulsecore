# Start from the latest golang base image
FROM golang:latest

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies
RUN go mod download

# Copy the proto files and client source files to the Working Directory inside the container
COPY proto/ proto/
COPY pulse-client/ pulse-client/

# Build the Go app
RUN go build -o main ./pulse-client/client

# Expose port for the application
EXPOSE 8080

# Command to run the executable
ENTRYPOINT ["./main"]
