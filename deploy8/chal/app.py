from http.server import SimpleHTTPRequestHandler, HTTPServer
import os

class Handler(SimpleHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/flag':
            self.send_response(200)
            self.end_headers()
            self.wfile.write(f"FLAG={os.environ.get('FLAG', 'NOFLAG')}".encode())
        else:
            super().do_GET()

if __name__ == '__main__':
    HTTPServer(('0.0.0.0', 8000), Handler).serve_forever()
