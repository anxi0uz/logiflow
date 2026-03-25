.PHONY: up build-up down deploy

up:
	podman-compose up -d

build-up:
	podman-compose build && podman-compose up -d

down:
	podman-compose down

deploy:
	podman-compose down && git pull && podman-compose build && podman-compose up -d
