import ws from 'k6/ws';
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter, Gauge } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');
const latency = new Trend('ws_latency');
const httpLatency = new Trend('http_latency');
const connectTime = new Trend('connect_time');
const messagesSent = new Counter('messages_sent');
const messagesReceived = new Counter('messages_received');
const activeConnections = new Gauge('active_connections');

// Test configuration
export const options = {
  stages: [
    { duration: '30s', target: 100 },   // Ramp up to 100 users
    { duration: '2m', target: 100 },    // Stay at 100 users
    { duration: '30s', target: 500 },   // Ramp to 500 users
    { duration: '2m', target: 500 },    // Stay at 500 users
    { duration: '30s', target: 1000 },  // Ramp to 1000 users
    { duration: '2m', target: 1000 },   // Stay at 1000 users
    { duration: '30s', target: 2000 },  // Stress test at 2000 users
    { duration: '1m', target: 2000 },   // Hold at 2000
    { duration: '1m', target: 0 },      // Ramp down
  ],
  thresholds: {
    'ws_latency': ['p(50)<50', 'p(95)<200', 'p(99)<500'],
    'http_latency': ['p(95)<100'],
    'errors': ['rate<0.01'], // Error rate < 1%
    'checks': ['rate>0.99'],  // Success rate > 99%
  },
};

// Test scenarios
const scenarios = [
  {
    name: 'tools_list',
    weight: 40,
    request: {
      jsonrpc: '2.0',
      method: 'tools/list',
      id: 1,
    },
  },
  {
    name: 'tools_call',
    weight: 30,
    request: {
      jsonrpc: '2.0',
      method: 'tools/call',
      params: {
        name: 'echo',
        arguments: { message: 'performance test' },
      },
      id: 2,
    },
  },
  {
    name: 'resources_list',
    weight: 20,
    request: {
      jsonrpc: '2.0',
      method: 'resources/list',
      id: 3,
    },
  },
  {
    name: 'prompts_list',
    weight: 10,
    request: {
      jsonrpc: '2.0',
      method: 'prompts/list',
      id: 4,
    },
  },
];

// Helper function to select scenario based on weights
function selectScenario() {
  const rand = Math.random() * 100;
  let cumulative = 0;
  
  for (const scenario of scenarios) {
    cumulative += scenario.weight;
    if (rand < cumulative) {
      return scenario;
    }
  }
  
  return scenarios[0];
}

// Main test function
export default function () {
  const gatewayUrl = __ENV.GATEWAY_URL || 'wss://localhost:8443/ws';
  const token = __ENV.AUTH_TOKEN || 'test-token';
  
  // Test HTTP health endpoint
  const healthCheck = http.get(`https://localhost:8443/health`, {
    headers: { 'Authorization': `Bearer ${token}` },
  });
  
  check(healthCheck, {
    'health check status is 200': (r) => r.status === 200,
    'health check has healthy status': (r) => {
      try {
        const body = JSON.parse(r.body);
        return body.status === 'healthy';
      } catch {
        return false;
      }
    },
  });
  
  if (healthCheck.timings) {
    httpLatency.add(healthCheck.timings.duration);
  }
  
  // WebSocket performance test
  const params = {
    headers: {
      'Authorization': `Bearer ${token}`,
    },
    tags: { test_type: 'websocket' },
  };
  
  const connectStart = Date.now();
  
  const res = ws.connect(gatewayUrl, params, function (socket) {
    const connectEnd = Date.now();
    connectTime.add(connectEnd - connectStart);
    activeConnections.add(1);
    
    socket.on('open', () => {
      // Send multiple requests to test multiplexing
      const numRequests = Math.floor(Math.random() * 5) + 1;
      const requestTimes = new Map();
      
      for (let i = 0; i < numRequests; i++) {
        const scenario = selectScenario();
        const request = {
          ...scenario.request,
          id: `${scenario.name}_${Date.now()}_${i}`,
        };
        
        requestTimes.set(request.id, Date.now());
        socket.send(JSON.stringify(request));
        messagesSent.add(1);
        
        sleep(Math.random() * 0.1); // Small delay between requests
      }
      
      // Keep connection open for a while to test connection stability
      socket.setTimeout(() => {
        socket.close();
      }, 5000 + Math.random() * 5000);
    });
    
    socket.on('message', (data) => {
      messagesReceived.add(1);
      
      try {
        const response = JSON.parse(data);
        
        // Calculate latency if we have the request time
        if (response.id && requestTimes.has(response.id)) {
          const duration = Date.now() - requestTimes.get(response.id);
          latency.add(duration);
          requestTimes.delete(response.id);
        }
        
        // Check response
        const success = check(response, {
          'response has no error': (r) => !r.error,
          'response has result or is notification': (r) => r.result !== undefined || r.method !== undefined,
        });
        
        if (!success || response.error) {
          errorRate.add(1);
        }
      } catch (e) {
        console.error('Failed to parse message:', e);
        errorRate.add(1);
      }
    });
    
    socket.on('close', () => {
      activeConnections.add(-1);
    });
    
    socket.on('error', (e) => {
      errorRate.add(1);
      console.error('WebSocket error:', e);
    });
  });
  
  check(res, {
    'websocket connection successful': (r) => r && r.status === 101,
  });
  
  // Random delay between virtual users
  sleep(Math.random() * 2 + 1);
}

// Setup function - runs once per VU
export function setup() {
  console.log('Starting performance test...');
  console.log(`Gateway URL: ${__ENV.GATEWAY_URL || 'wss://localhost:8443/ws'}`);
  console.log(`Target stages: ${JSON.stringify(options.stages)}`);
  
  // Warm up the system
  const warmupRequests = 10;
  for (let i = 0; i < warmupRequests; i++) {
    http.get('https://localhost:8443/health');
  }
  
  return {
    startTime: Date.now(),
  };
}

// Teardown function - runs once after all iterations
export function teardown(data) {
  const duration = (Date.now() - data.startTime) / 1000;
  console.log(`Test completed in ${duration} seconds`);
}