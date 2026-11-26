# grade-frontend (Vite build -> nginx; контекст сборки — services/frontend)
FROM node:22-alpine AS build
RUN corepack enable
WORKDIR /app
COPY package.json pnpm-lock.yaml* ./
RUN pnpm install --frozen-lockfile || pnpm install
COPY . .
RUN pnpm build

FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
# 127.0.0.1, а не localhost: nginx слушает IPv4 (0.0.0.0:80), а localhost в
# контейнере первым делом резолвится в ::1 — проба падала «connection refused»,
# и nginx считался unhealthy, хотя SPA отдавалась нормально.
HEALTHCHECK --interval=30s --timeout=5s --retries=3 CMD wget -qO- http://127.0.0.1:80/ || exit 1
