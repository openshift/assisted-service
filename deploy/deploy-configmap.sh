CONFIGMAP=./deploy/tmp-bm-inventory-configmap.yaml
URL=$(minikube service bm-inventory --url| sed 's/http:\/\///g' | cut -d ":" -f 1)
PORT=$(minikube service bm-inventory --url| sed 's/http:\/\///g' | cut -d ":" -f 2)
sed "s#REPLACE_URL#\"$URL\"#;s#REPLACE_PORT#\"$PORT\"#" ./deploy/bm-inventory-configmap.yaml > $CONFIGMAP
echo "Apply bm-inventory-config configmap"
cat $CONFIGMAP
kubectl apply -f $CONFIGMAP
rm $CONFIGMAP
