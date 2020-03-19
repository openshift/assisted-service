CONFIGMAP=./deploy/s3/tmp-scality-configmap.yaml
URL=$(minikube service scality --url) || URL="https://scality:8000"
sed "s#REPLACE_URL#$URL#" ./deploy/s3/scality-configmap.yaml > $CONFIGMAP
cat $CONFIGMAP
kubectl apply -f $CONFIGMAP
rm $CONFIGMAP
