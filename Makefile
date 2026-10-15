.PHONY: up build-up down deploy seed

up:
	podman-compose up -d

build-up:
	podman-compose build && podman-compose up -d

down:
	podman-compose down

seed:
	LOGIFLOW_DATABASE_HOST=127.0.0.1 go run ./cmd/seed

deploy:
	podman-compose down && git pull && podman-compose build && podman-compose up -d
