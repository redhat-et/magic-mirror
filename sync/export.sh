#!/bin/bash

set -e -o pipefail

if [ $PROVIDERTYPE == 'AWS' ]; then
    echo "Syncing from AWS"
    until s5cmd ls ${STORAGEOBJECT}; do
      echo "Cannot access ${STORAGEOBJECT} sleeping and requeuing..."
      sleep 30;
    done
    s5cmd sync /data/ ${STORAGEOBJECT}
    chmod 666 -R /data
fi
