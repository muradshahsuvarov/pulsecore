@echo off
psql -U postgres -h localhost -a -f dbsetup.sql
pause
