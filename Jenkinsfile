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


  stage('Deploy to prod') {
    when {
      branch 'master'
    }
    agent any
        steps {
         withCredentials([usernameColonPassword(credentialsId: '603600b1-7ba3-471f-be8f-0f7a1ec4871c', variable: 'PASS')]) {
          sh '''docker login quay.io -u ocpmetal -p $PASS'''
        }
          sh '''docker push quay.io/ocpmetal/bm-inventory'''

        }
    }
}
  post {
          failure {
              echo 'Get assisted-service log'
              sh '''
              kubectl get pods -o=custom-columns=NAME:.metadata.name -A | grep assisted-service | xargs -I {} sh -c "kubectl logs {} -n  assisted-installer > test_dd.log"
              mv test_dd.log $WORKSPACE/assisted-service.log || true
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
