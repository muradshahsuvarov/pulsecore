#!/bin/bash

protoc --proto_path=../../proto --go_out=../../proto/proto --go_opt=paths=source_relative --go-grpc_out=../../proto/proto --go-grpc_opt=paths=source_relative message.proto