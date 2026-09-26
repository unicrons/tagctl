FROM node:22-bookworm-slim

ENV PUPPETEER_SKIP_DOWNLOAD=true

RUN npm install --global mint@4.2.313

WORKDIR /docs

EXPOSE 3000

CMD ["mint", "dev", "--no-open"]
