<img src="./assets/logo.jpg" alt="PulseCore Logo" width="200"/>

# PulseCore: The Open-Source Game Server

PulseCore is an innovative, open-source game server built to revolutionize the way multiplayer games interact with backend infrastructure. PulseCore brings together the efficiency of RPC with the robustness of Apache Kafka, all while integrating AI functionalities to deliver high-speed and immersive gaming experiences.

## Features

- **RPC & Kafka Integration**: Experience real-time game state synchronization across diverse player bases.
- **Geographically Distributed**: Optimal latency management by deploying in regions of choice including AWS, GCP, Azure, and others.
- **AI-Powered Enhancements**: Leverage enhanced chat systems, dynamic game narratives, and predictive player assistance.
- **Granular Analytics**: Keep an eye on CCU, DAY, AAU, and a suite of essential metrics in real-time.
- **Broad SDK Support**: Ready-to-use SDKs for popular game engines such as Unity and Unreal.
- **Microservices Centric**: Designed with scalability, resilience, and maintenance as a forefront.
- **Community & Open-Source**: A platform built for developers, endorsed by developers.

## Quick Start

1. **Clone**:
   ```bash
   git clone https://github.com/muradshahsuvarov/pulsecore
   ```

2. **Endpoint Documentation**

    - Endpoint documentation can be accessible locally and remotely.
    - You can import the postam based documentation from the following subdirectory: `.\services\endpoint_documentations`
    - The remote version can be accessed from here: https://documenter.getpostman.com/view/9501436/2s9YRCXrn3

3. **Database Setup for the PulseCore SaaS**:
   
   Before running PulseCore, ensure your database is properly configured. To set up the necessary PostgreSQL tables and schemas, simply run the provided setup script:

   For Windows users:
   ```
   Execute the dbsetup.bat script to initialize the necessary PostgreSQL tables.
   ```

   For Unix/Linux/macOS users:
   ```bash
   Execute the dbsetup.sh script to initialize the necessary PostgreSQL tables.
   ```

   These scripts encapsulate all the required table schemas and database configurations for a hassle-free setup.

4. **Services Setup**

    Before running the gRPC related service you have to run the database and middleware service on your local machine

    - Execute the following command: `go run ./main.go` in `./services/database/src` and `./services/middleware/src`
    - Follow the endpoint description and create a user, authenticate, create an application, create a server (127.0.0.1:12345)

## Deployment & Scaling

### Redis Setup

1. Pull Redis Image
   ```
   docker pull redis
   ```

2. Run Redis Container
   ```
   docker run -d --name <container-name> --network <shared-network> -p <desired-port>:<server-port> redis:latest
   ```
   Example:
   ```
   docker run -d --name redis01 --network pulsecore_network -p 6379:6379 redis:latest
   ```

### Server Setup

1. Navigate to the server directory:
    ```bash
    cd pulsecore/servers
    ```

2. Build the Docker image for the server:
    ```bash
    docker build -t pulsecore_server -f servers/Dockerfile .
    ```

3. Run the server:
    ```bash
    docker run -d -e SERVER_ADDR=<server-address>:<server-port> -e REDIS_ADDR=<redis-address> --network <shared-docker-network> -p <desired-port>:<server-port> --name <container-name> pulsecore_server:latest
    ```
    Example:
    ```
    docker run -d -e SERVER_ADDR="0.0.0.0:12345" -e REDIS_ADDR="redis01:6379" --network pulsecore_network -p 12345:12345 --name pulsecore_server_0 pulsecore_server:latest
    ```
    Note: `0.0.0.0` allows the service to accept connections from any IP address on any network interface of the machine.

### Matchmaking Server Setup

1. Run the following command to build an image for the matchmaking:
    ```
    docker build -t matchmaking_server -f ./services/matchmaking/Dockerfile .
    ```
2. Run the following command to run a container:
    ```
    docker run -d -e SERVER_ADDR=":8096" -e REDIS_ADDR="redis01:6379" --network pulsecore_network -p 12354:8096 --name pulsecore_mm_server_0  matchmaking_server:latest
    ```

### Client Setup

1. Navigate to the client directory:
    ```bash
    cd pulsecore/pulse-client/client
    ```

2. Build the Docker image for the client:
    ```bash
    docker build -t pulsecore_client -f pulse-client/Dockerfile 
    ```

3. Run the client (repeat for multiple clients):

    - Navigate to client folder

    ```bash
    docker run -it --name pulsecore_client_Murad --network <shared-network> -p <desired-port>:<server-port> pulsecore_client --server=<container-name>:<container-port> --redis-server=<redis-container>:<redis-port> --name=Murad
    ```
    Example:
    ```
    docker run -it --name pulsecore_client_Murad --network pulsecore_network -p 8001-9000 pulsecore_client --server=pulsecore_server_0:12345 --redis-server=redis01:6379 --name=Murad
    ```
	
4. To run client locally (Local Matchmaking) use:

    - Navigate to local-client folder

	```
    go run main.go --app <REGISTERED_APPLICATION_ID> --server <REGISTERED_SERVER_ADDRESS> --redis-server 127.0.0.1:6379
    ```

    ```
	go run main.go --app b5864d14-08e6-4e3e-82a5-33b91d2bb985 --server 127.0.0.1:12345 --redis-server 127.0.0.1:6379
	```

5. To run client locally (Remote Matchmaking) use:

    - Navigate to local-client-remote-mm folder

	```
    go run main.go --app <REGISTERED_APPLICATION_ID> --server <REGISTERED_SERVER_ADDRESS>
    ```

    ```
	go run main.go --app b5864d14-08e6-4e3e-82a5-33b91d2bb985 --server 127.0.0.1:12345
	```

### Generation of Golang client and server based on proto
   1. Navigate to the client directory:
    ```bash
    cd proto/
    ```

   2. Run the following command
   ```
   protoc --go_out=./proto/ --go_opt=paths=source_relative --go-grpc_out=./proto/ --go-grpc_opt=paths=source_relative ./proto/message.proto
   ```

### IMPROTANT

   Make sure Redis, Server, and Clients are deployed on the same network


## Licensing

### Open Source License

PulseCore is free and open-source under the [MIT License](LICENSE). This allows you to use PulseCore in non-commercial projects. However, if you intend to use PulseCore for any commercial purposes, you must obtain a commercial license.

**Important**: Unauthorized commercial use without obtaining the appropriate license is strictly prohibited.

## Documentation

Explore our comprehensive [Documentation](/docs) for API references, user guides, and interactive tutorials.

## Contribute to PulseCore

Global developers are the backbone of PulseCore. Contributions, feedback, and insights are always welcome.

1. Fork and clone.
2. Branch out.
3. Commit & PR.

Dive into our [CONTRIBUTING.md](/community/CONTRIBUTING.md) for detailed guidelines.

## Community & Support

Engage with developers and gamers in our [Discord server](#). Let's shape the future of gaming together!

## Cheers to the Community!

A massive shoutout to developers, contributors, and the vibrant PulseCore community. Let's game on!

© 2023 Murad Shahsuvarov
