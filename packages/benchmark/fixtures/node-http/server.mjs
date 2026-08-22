import http from 'node:http';

const port = Number(process.env.PORT ?? 3000);
const body = JSON.stringify({ message: 'hello', id: 42 });

const server = http.createServer((req, res) => {
  if (req.url === '/api/json') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(body);
  } else {
    res.writeHead(404);
    res.end();
  }
});

server.listen(port, '127.0.0.1', () => {
  console.log(`listening on ${port}`);
});
