-- Create the pulsecoredb database
CREATE DATABASE pulsecoredb;

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

-- Create the server_addresses table
CREATE TABLE server_addresses (
    id SERIAL PRIMARY KEY,
    address VARCHAR(255) NOT NULL UNIQUE,
    app_id INT NOT NULL REFERENCES applications(id),
    tag VARCHAR(255) NOT NULL UNIQUE,
    date_added TIMESTAMP NOT NULL DEFAULT current_timestamp
);
