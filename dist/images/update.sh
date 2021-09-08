nbleader=$(kubectl -n kube-system get pods -l ovn-nb-leader=true -o jsonpath='{.items[*].metadata.name}')
echo "leader is " "${nbleader}"
kubectl -n kube-system exec -ti "${nbleader}" -- bash /kube-ovn/add-label.sh
echo "update finished"
