./bin/mqttbridge --master tcp://localhost:1883 --slave tcp://localhost:1884 --topic "test/#" > bridge.log 2>&1 &
BRIDGE_PID=$!
sleep 2
mosquitto_pub -h localhost -p 1883 -t "test/proxy" -m '{"msg": "test forward"}'
sleep 1
kill $BRIDGE_PID
cat bridge.log
