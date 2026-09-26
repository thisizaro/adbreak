.PHONY: web build run dev

web:
	cd web && npm run build
	touch web/dist/.gitkeep

build: web
	go build -o bin/adbreak ./cmd/server

run: build
	./bin/adbreak
