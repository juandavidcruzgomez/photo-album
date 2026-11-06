# The web app is built into web/dist and embedded in the api binary.

# Use the Node version in web/.nvmrc when nvm is installed; otherwise the
# node on PATH, which must be 20.19+ or 22.12+ (required by Vite).
NVM_EXEC := $(wildcard $(or $(NVM_DIR),$(HOME)/.nvm)/nvm-exec)
NODE_OK := const [a,b]=process.versions.node.split(".").map(Number); \
	process.exit((a===20&&b>=19)||(a===22&&b>=12)||a>22 ? 0 : 1)
NPM := cd web && { $(NVM_EXEC) node -e '$(NODE_OK)' \
	|| { echo "Node $$($(NVM_EXEC) node -v) is not supported: use 20.19+ or 22.12+ (see web/.nvmrc)" >&2; exit 1; }; } \
	&& $(NVM_EXEC) npm

COMPOSE := docker compose -f ../docker/docker-compose.local.yml

.PHONY: web api gen-api dev-web test images up crawl rpi-setup rpi-images rpi-deploy indexer-up indexer-check

web: web/node_modules
	$(NPM) run build

web/node_modules: web/package-lock.json
	$(NPM) ci
	@touch $@

api: web
	go build -o bin/api ./cmd/api

# Regenerate the web app's API types after editing api/openapi.yaml.
gen-api:
	$(NPM) run gen:api

# Vite dev server on :5173, proxying /api to the API on :8090.
dev-web:
	$(NPM) run dev

test:
	go test ./...
	$(NPM) run typecheck

# --- Docker -------------------------------------------------------------------

# Build the api, indexer and crawler images (models included).
images:
	$(COMPOSE) --profile tools build api indexer crawler

# Start everything: storage, database, api (http://localhost:8090) and indexer.
up:
	$(COMPOSE) up -d --build

# Queue a folder for indexing: make crawl DIR=/home/me/Pictures
# DIR must be under PHOTOS_ROOT (default: $$HOME).
crawl:
	@test -n "$(DIR)" || { echo "usage: make crawl DIR=/absolute/path" >&2; exit 1; }
	$(COMPOSE) run --rm crawler "$(abspath $(patsubst ~%,$(HOME)%,$(DIR)))"

# --- Raspberry Pi + PC --------------------------------------------------------

RPI_DIR ?= phalbum

# Install arm64 emulation for Docker builds. Repeat after a reboot.
rpi-setup:
	docker run --privileged --rm tonistiigi/binfmt --install arm64

# Build the arm64 api and crawler images for the Raspberry Pi.
rpi-images:
	docker buildx build --platform linux/arm64 --target api -t phalbum-api:arm64 --load .
	docker buildx build --platform linux/arm64 --target crawler -t phalbum-crawler:arm64 --load .

# Copy the images and compose file to the Pi: make rpi-deploy RPI=user@host
rpi-deploy:
	@test -n "$(RPI)" || { echo "usage: make rpi-deploy RPI=user@host [RPI_DIR=phalbum]" >&2; exit 1; }
	docker save phalbum-api:arm64 phalbum-crawler:arm64 | gzip | ssh $(RPI) 'gunzip | docker load'
	ssh $(RPI) 'mkdir -p $(RPI_DIR)'
	scp ../docker/docker-compose.rpi.yml ../docker/rpi.env.example ../docker/s3.example.json $(RPI):$(RPI_DIR)/

# Start the indexer on this machine, using the database and storage on the Pi.
indexer-up:
	cd ../docker && docker compose --env-file indexer.env -f docker-compose.indexer.yml up -d --build

# Check this machine's indexer can reach the database, storage and photos.
indexer-check:
	cd ../docker && docker compose --env-file indexer.env -f docker-compose.indexer.yml run --rm --build indexer -check
