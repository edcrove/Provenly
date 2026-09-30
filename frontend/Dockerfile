# syntax=docker/dockerfile:1
# Provenly UI. Targets:
#   dev     - Vite dev server with hot module replacement (source bind-mounted).
#   runtime - nginx serving the production build and proxying /api to the API (default).
ARG NODE_VERSION=22

FROM node:${NODE_VERSION}-alpine AS deps
# Optional extra CA (corporate TLS-intercepting proxy): `--secret id=extra_ca,src=<pem>`.
RUN --mount=type=secret,id=extra_ca,required=false \
    if [ -s /run/secrets/extra_ca ]; then cat /run/secrets/extra_ca >> /etc/ssl/certs/ca-certificates.crt; fi
ENV NODE_EXTRA_CA_CERTS=/etc/ssl/certs/ca-certificates.crt
WORKDIR /app
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci

FROM deps AS dev
ENV PROVENLY_API_URL=http://api:8080
EXPOSE 5173
CMD ["npx", "vite", "--host", "0.0.0.0", "--port", "5173", "--strictPort"]

FROM deps AS build
COPY . .
RUN npm run build

FROM nginx:alpine AS runtime
ENV API_UPSTREAM=http://api:8080
COPY nginx.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=5s --timeout=3s --retries=20 CMD wget -qO- http://127.0.0.1/ >/dev/null || exit 1
