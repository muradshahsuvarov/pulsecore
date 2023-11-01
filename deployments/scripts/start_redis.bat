@echo off
SET SERVER_ID=%1
SET /A REDIS_PORT=6000+%SERVER_ID%

echo port %REDIS_PORT% > redis_%SERVER_ID%.conf

start redis-server redis_%SERVER_ID%.conf
