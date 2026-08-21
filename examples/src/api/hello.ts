import type { IncomingMessage, ServerResponse } from 'http';

export default function handler(req: IncomingMessage, res: ServerResponse) {
  res.statusCode = 200;
  res.setHeader('Content-Type', 'application/json');
  
  return res.end(JSON.stringify({ 
    message: 'Hello from your custom krate API route!',
    method: req.method,
    timestamp: Date.now()
  }));
}