#!/bin/bash
cp ./src/scheduler/main.go ./cmd/scheduler/main.go 
docker build -t smvfal/custom-scheduler:latest .
docker push smvfal/custom-scheduler

