#!/bin/bash

if [ $SOURCETYPE == 'AWS' ]; then
    echo "Syncing from AWS"
    aws s3 sync s3://${SOURCE} /data
elif [ $SOURCETYPE == 'HTTP' ]; then
    echo "Syncing from HTTP"
    wget -r -np -nH -P /data ${SOURCE}
elif [ $SOURCETYPE == 'SSH' ]; then
    echo "Syncing from SSH"
    rsync -avz -e "ssh -i ${{SSH_KEY}}" ${SOURCE} /data
fi
