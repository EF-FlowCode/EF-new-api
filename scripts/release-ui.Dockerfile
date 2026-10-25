FROM mcr.microsoft.com/playwright:v1.58.2-noble
WORKDIR /opt/release-ui
ENV PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
RUN npm install --ignore-scripts --no-audit --no-fund --save-exact playwright@1.58.2 mysql2@3.14.4
ENV NODE_PATH=/opt/release-ui/node_modules
WORKDIR /src
