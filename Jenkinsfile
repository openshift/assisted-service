pipeline {
	agent { label 'bm-inventory-subsystem' }
	environment {
		SERVICE = 'ocpmetal/assisted-service'
	}


	stages {
		stage('clear deployment') {
			steps {
				sh 'make clear-deployment'
				sh 'echo ${SERVICE}'
			}
		}

		stage('Build') {
			steps {
				sh '''export PATH=$PATH:/usr/local/go/bin; make echo-build'''
			}
		}
	}

}
