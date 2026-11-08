# The web app is built into web/dist and embedded in the api binary.

# Use the Node version in web/.nvmrc when nvm is installed; otherwise the
# node on PATH, which must be 20.19+ or 22.12+ (required by Vite).
NVM_EXEC := $(wildcard $(or $(NVM_DIR),$(HOME)/.nvm)/nvm-exec)
NODE_OK := const [a,b]=process.versions.node.split(".").map(Number); \
	process.exit((a===20&&b>=19)||(a===22&&b>=12)||a>22 ? 0 : 1)
NPM := cd web && { $(NVM_EXEC) node -e '$(NODE_OK)' \
	|| { echo "Node $$($(NVM_EXEC) node -v) is not supported: use 20.19+ or 22.12+ (see web/.nvmrc)" >&2; exit 1; }; } \
	&& $(NVM_EXEC) npm

.PHONY: web api gen-api dev-web test

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
