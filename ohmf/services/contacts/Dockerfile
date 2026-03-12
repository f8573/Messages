FROM golang:1.25-alpine AS build
WORKDIR /src/ohmf
COPY ohmf /src/ohmf
RUN go build -o /bin/contacts ./services/contacts

FROM alpine:latest
COPY --from=build /bin/contacts /bin/contacts
EXPOSE 18085
CMD ["/bin/contacts"]
