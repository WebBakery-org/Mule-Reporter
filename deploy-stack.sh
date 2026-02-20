# first arg should be your stack's name

if [ -n "$MULE_REGISTRY" ]; then
    export MULE_FULL_REGISTRY="${MULE_REGISTRY}/"
else
    # Sinon, on laisse vide
    export MULE_FULL_REGISTRY=""
fi

./build-image.sh
docker stack deploy --detach=true --resolve-image=never -c docker/docker-stack.yml $1