import utils

def main():
    print(utils.check_output("kubectl delete all --all -n assisted-installer 1> /dev/null ; true"))
    print(utils.check_output("kubectl delete namespace assisted-installer 1> /dev/null ; true"))

if __name__ == "__main__":
    main()
