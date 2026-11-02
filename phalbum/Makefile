# The web app is built into web/dist and embedded in the api binary.
NPM := cd web && npm

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
