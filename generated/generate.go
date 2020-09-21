package generated

/* Use this file to addmock generation tags for artifacts that are themselves generated and can not be
 * decorated with tags in their original location 
 */

 //go:generate mockgen -source configure_assisted_install.go -package generated -destination mock_InstallerAPI github.com/openshift/assisted-service/restapi InstallerAPI