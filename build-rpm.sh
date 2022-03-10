#!/bin/bash -x

echo "Collect the source into a tarball"

export VERSION="0.1"

if [ "${BUILD_NUMBER}" = "" ]
then
	export BUILD_NUMBER=0
fi	

mkdir -p SOURCES

tar czvf SOURCES/cosmos-exporter-${VERSION}.tar.gz dashboards images *.md *.go *.mod *.sum *.abi

echo "Build the package"

rpmbuild --define "_topdir $( pwd )" --define "_versiontag ${VERSION}" --define "_releasetag ${BUILD_NUMBER}" -ba $( pwd )/SPECS/cosmos-exporter.spec

