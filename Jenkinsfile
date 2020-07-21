pipeline {
  agent { label 'bm-inventory-subsystem' }
  stages {
    stage('clear deployment') {
      steps {
        sh 'make clear-deployment'
      }
    }

    stage('Deploy') {
      steps {
        sh '''export PATH=$PATH:/usr/local/go/bin; export OBJEXP=quay.io/ocpmetal/s3-object-expirer:latest; make deploy-test'''
        sleep 60
        sh '''# Dump pod statuses;kubectl  get pods -A'''
      }
    }

    stage('test') {
      steps {
        sh '''export PATH=$PATH:/usr/local/go/bin;make subsystem-run'''
      }
    }
  }
  post {
          failure {
              echo 'Get bm-inventory log'
              sh '''
              kubectl get pods -o=custom-columns=NAME:.metadata.name -A | grep bm-inventory | xargs -I {} sh -c "kubectl logs {} -n  assisted-installer > test_dd.log"
              mv test_dd.log $WORKSPACE/bm-inventory.log || true
              '''

              echo 'Get mariadb log'
              sh '''kubectl  get pods -o=custom-columns=NAME:.metadata.name -A | grep mariadb | xargs -I {} sh -c "kubectl logs {} -n  assisted-installer > test_dd.log"
              mv test_dd.log $WORKSPACE/mariadb.log || true
              '''

              echo 'Get createimage log'
              sh '''kubectl  get pods -o=custom-columns=NAME:.metadata.name -A | grep createimage | xargs -I {} sh -c "kubectl logs {} -n  assisted-installer > test_dd.log"
              mv test_dd.log $WORKSPACE/createimage.log || true
              '''
          }
  }
}