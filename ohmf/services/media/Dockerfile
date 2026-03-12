FROM golang:1.25-alpine AS build
WORKDIR /src/ohmf
COPY ohmf /src/ohmf
RUN go build -o /bin/media ./services/media

FROM alpine:latest
COPY --from=build /bin/media /bin/media
EXPOSE 18087
CMD ["/bin/media"]
