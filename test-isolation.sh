#!/bin/bash
echo "Testing worklet isolation..."
echo "Running 'docker ps' inside the container should show an empty list in full isolation mode"
echo ""
./worklet run