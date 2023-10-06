-- Create the pulsecoredb database
CREATE DATABASE pulsecoredb2;

-- Switch to the pulsecoredb database
\c pulsecoredb;

-- Create the users table
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(50) NOT NULL UNIQUE,
    password VARCHAR(255) NOT NULL,
    date_created TIMESTAMP NOT NULL DEFAULT current_timestamp
);

-- Create the applications table
CREATE TABLE applications (
    id SERIAL PRIMARY KEY,
    app_name VARCHAR(255) NOT NULL,
    app_identifier VARCHAR(100) NOT NULL UNIQUE,
    app_description TEXT,
    user_id INT NOT NULL REFERENCES users(id),
    date_created TIMESTAMP NOT NULL DEFAULT current_timestamp,
    last_updated TIMESTAMP NOT NULL DEFAULT current_timestamp
);

-- Create the game_stats table
CREATE TABLE game_stats (
    id SERIAL PRIMARY KEY,
    user_id INT NOT NULL REFERENCES users(id),
    game_played INT NOT NULL,
    properties JSONB,
    last_played TIMESTAMP DEFAULT current_timestamp
);

-- Create the rooms table
CREATE TABLE rooms (
    room_id SERIAL PRIMARY KEY,
    room_name VARCHAR(50) NOT NULL,
	host_id INT NOT NULL REFERENCES users(id),
    max_players INT NOT NULL DEFAULT 10,
    current_players INT DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'available',
    properties JSONB,
    date_created TIMESTAMP NOT NULL DEFAULT current_timestamp
);
