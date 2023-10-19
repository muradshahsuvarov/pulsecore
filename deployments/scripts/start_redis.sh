#!/bin/bash

SERVER_ID=$1
REDIS_PORT=$((6000 + $SERVER_ID))

echo "port $REDIS_PORT" > redis_$SERVER_ID.conf

# Start Redis
redis-server redis_$SERVER_ID.conf
