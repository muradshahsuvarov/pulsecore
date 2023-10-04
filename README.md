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

1. **Database Setup**:
   
   Before running PulseCore, ensure you've set up the necessary PostgreSQL tables. The following table schemas need to exist:

   ```sql
   CREATE TABLE users (
       user_id SERIAL PRIMARY KEY,
       username VARCHAR(50) NOT NULL UNIQUE,
       password VARCHAR(255) NOT NULL,
       email VARCHAR(100) UNIQUE,
       date_created TIMESTAMP DEFAULT current_timestamp
   );

   CREATE TABLE game_stats (
       stat_id SERIAL PRIMARY KEY,
       user_id INT REFERENCES users(user_id),
       game_played VARCHAR(50),
       properties JSONB,
       date_played TIMESTAMP DEFAULT current_timestamp
   );

   CREATE TABLE rooms (
       room_id SERIAL PRIMARY KEY,
       room_name VARCHAR(50) NOT NULL,
       max_players INT DEFAULT 10,
       current_players INT DEFAULT 0,
       status VARCHAR(20) DEFAULT 'available',
       properties JSONB,
       date_created TIMESTAMP DEFAULT current_timestamp
   );
   ```

2. **Clone**:
   ```bash
   git clone https://github.com/muradshahsuvarov/pulsecore
   ```

3. **Setup**:
   ```bash
   cd pulsecore/server && go get .
   ```

4. **Run**: -

## Licensing

### Open Source License

PulseCore is free and open-source under the [MIT License](LICENSE). This allows you to use PulseCore in non-commercial projects. However, if you intend to use PulseCore for any commercial purposes, you must obtain a commercial license.

### Commercial License

If you wish to use PulseCore in a commercial game or application, please obtain a commercial license. The commercial license provides additional features, support, and ensures your game scales while supporting PulseCore's continued development. For more details and pricing, please [contact us](mailto:muradshahsuvarov@gmail.com).

**Important**: Unauthorized commercial use without obtaining the appropriate license is strictly prohibited.

## Documentation

Explore our comprehensive [Documentation](/docs) for API references, user guides, and interactive tutorials.

## Deployment & Scaling

Designed for the cloud! Deploy using Docker and orchestrate with Kubernetes for global scale. Detailed guides available in the [`deployment`](/deployment) directory.

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
