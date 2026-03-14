from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import os


class NoCacheHandler(SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
        self.send_header("Pragma", "no-cache")
        self.send_header("Expires", "0")
        super().end_headers()


def main():
    port = int(os.environ.get("CLIENT_PORT", "5173"))
    server = ThreadingHTTPServer(("0.0.0.0", port), NoCacheHandler)
    server.serve_forever()


if __name__ == "__main__":
    main()
