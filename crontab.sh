#!/bin/sh
ps -ef | grep go | grep coin-usdt | grep -v grep
if [ $? -ne 0 ]
then
/root/go/src/coin-v6/coin-usdt >> /tmp/coin.log
else
echo "runing......"
fi
