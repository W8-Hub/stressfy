FROM node:20-alpine AS deps

WORKDIR /app

COPY package*.json ./
RUN npm install

FROM deps AS build

COPY tsconfig.json ./
COPY src ./src
RUN npm run build

FROM node:20-alpine AS runner

WORKDIR /app

ENV NODE_ENV=production
ENV PORT=3333
ENV DATA_DIR=/tmp/stress-api
ENV TZ_OFFSET=-03:00

COPY package*.json ./
RUN npm install --omit=dev

COPY --from=build /app/dist ./dist

EXPOSE 3333

CMD ["node", "dist/index.js"]