#!/bin/bash

echo "Testing CORS implementation for worklet terminal server"
echo "======================================================="

# Test 1: Check CORS headers on /api/forks endpoint
echo -e "\n1. Testing CORS headers on /api/forks endpoint:"
curl -i -X GET http://localhost:8080/api/forks 2>/dev/null | grep -E "Access-Control-Allow-Origin|Access-Control-Allow-Methods|Access-Control-Allow-Headers|Access-Control-Max-Age" || echo "CORS headers not found"

# Test 2: Test preflight OPTIONS request
echo -e "\n2. Testing preflight OPTIONS request:"
curl -i -X OPTIONS http://localhost:8080/api/forks \
  -H "Origin: http://example.com" \
  -H "Access-Control-Request-Method: GET" \
  -H "Access-Control-Request-Headers: Content-Type" 2>/dev/null | head -20

# Test 3: Test cross-origin request
echo -e "\n3. Testing cross-origin GET request:"
curl -i -X GET http://localhost:8080/api/forks \
  -H "Origin: http://example.com" 2>/dev/null | head -15

echo -e "\nNote: Make sure the terminal server is running with: ./worklet terminal"
echo "You can test custom CORS origin with: ./worklet terminal --cors-origin='https://myapp.com'"