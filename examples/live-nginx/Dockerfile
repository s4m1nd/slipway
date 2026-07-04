FROM nginx:alpine

ARG MOCK_VERSION=unknown

RUN printf 'Slipway mock %s\n' "$MOCK_VERSION" > /usr/share/nginx/html/index.html
COPY default.conf /etc/nginx/conf.d/default.conf
