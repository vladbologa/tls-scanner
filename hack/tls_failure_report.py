#!/usr/bin/env python3
"""
script to fetch tls-related test failures from openshift prow ci.

this script fetches and analyzes tls-related test failures from openshift's
prow ci. it scrapes the latest job results for two periodic tests, parses junit
xml files to extract failed pod names, deduplicates them by stripping
kubernetes hash suffixes, and generates a report categorizing which components
are failing which tests.

tests:
 tls 1.3 adherence (ocp 5.0 requirement):
  job: periodic-ci-openshift-tls-scanner-main-periodic-tls13-adherence

 pqc readiness (ocp 4.22 requirement):
  job: periodic-ci-openshift-tls-scanner-main-periodic-pqc-readiness

usage:
    python3 hack/tls_failure_report.py

the script will fetch the latest test results and write a detailed report to
tls_failure_report.txt in the current directory.
"""

import re
import sys
from urllib.request import urlopen
import xml.etree.ElementTree as ET


def fetch_latest_job_id(job_history_url):
    """fetch the job history page and extract the latest job id."""
    print(f"fetching job history from: {job_history_url}")

    with urlopen(job_history_url) as response:
        html = response.read().decode('utf-8')

    # the job ids are in a javascript variable called allbuilds
    match = re.search(r'var allBuilds = \[(.*?)\];', html, re.DOTALL)

    if not match:
        print("could not find allbuilds variable in page")
        return None

    builds_json = '[' + match.group(1) + ']'

    # extract all ids from the json
    id_pattern = re.compile(r'"ID":"(\d+)"')
    job_ids = id_pattern.findall(builds_json)

    if not job_ids:
        print("no job ids found in allbuilds")
        return None

    # the first one is the latest (most recent)
    latest_job_id = job_ids[0]
    print(f"latest job id: {latest_job_id}")
    return latest_job_id


def fetch_junit_xml_urls(job_result_url, job_name, job_id):
    """fetch the job result page and extract junit xml artifact urls."""
    print(f"fetching job results from: {job_result_url}")

    with urlopen(job_result_url) as response:
        html = response.read().decode('utf-8')

    # extract junit xml paths from the lensartifacts javascript variable
    match = re.search(r'var lensArtifacts = ({.*?});', html, re.DOTALL)

    if not match:
        print("could not find lensartifacts in page")
        return []

    artifacts_json = match.group(1)

    # extract all paths that contain junit
    junit_pattern = re.compile(r'"(artifacts/[^"]*junit[^"]*\.xml)"')
    junit_paths = junit_pattern.findall(artifacts_json)

    # build full urls to the junit xml files
    base_url = f"https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/logs/{job_name}/{job_id}"
    junit_urls = [f"{base_url}/{path}" for path in junit_paths]

    print(f"found {len(junit_urls)} junit xml files")
    return junit_urls


def parse_junit_xml(xml_url):
    """parse a junit xml file and extract failed test pod names."""
    print(f"  parsing: {xml_url.split('/')[-1]}")

    try:
        with urlopen(xml_url) as response:
            xml_content = response.read()

        root = ET.fromstring(xml_content)
        pod_names = set()

        # find all testcase elements with failure child elements
        for testcase in root.findall('.//testcase'):
            failure = testcase.find('failure')
            if failure is not None:
                classname = testcase.get('classname', '')
                if classname:
                    pod_names.add(classname)

        return pod_names
    except Exception as e:
        print(f"  error parsing xml: {e}")
        return set()


def strip_pod_hash(pod_name):
    """strip pod hash suffixes from kubernetes pod names.

    examples:
        aws-ebs-csi-driver-node-crb5b -> aws-ebs-csi-driver-node
        console-58956bdd98-8qnvp -> console
        service-ca-849bcb7457-h2pfr -> service-ca
        loki-promtail-8pjm2 -> loki-promtail
    """
    if not pod_name:
        return pod_name

    parts = pod_name.split('-')

    # need at least 2 parts to strip
    if len(parts) < 2:
        return pod_name

    def looks_like_hash(s):
        """check if a string looks like a kubernetes hash."""
        if not s or not s.isalnum():
            return False
        # mixed letters and digits is a hash
        has_digit = any(c.isdigit() for c in s)
        has_alpha = any(c.isalpha() for c in s)
        if has_digit and has_alpha:
            return True
        # short lowercase-only strings (5-6 chars) are also likely pod hashes
        if 5 <= len(s) <= 6 and s.islower() and s.isalpha():
            return True
        return False

    # check if last part is a hash (5-10 chars)
    last_part = parts[-1]
    if len(last_part) >= 5 and len(last_part) <= 10 and looks_like_hash(last_part):
        parts = parts[:-1]

    # check if the new last part is also a hash (replicaset hash for deployments)
    # replicaset hashes are typically 8-10 chars with mixed letters and numbers
    if len(parts) >= 2:
        last_part = parts[-1]
        if len(last_part) >= 8 and len(last_part) <= 12 and looks_like_hash(last_part):
            parts = parts[:-1]

    return '-'.join(parts)


def fetch_failed_pods(job_name, job_path):
    """fetch failed pods for a specific job."""
    base_url = "https://prow.ci.openshift.org"
    job_history_url = base_url + job_path

    # fetch latest job id
    latest_job_id = fetch_latest_job_id(job_history_url)
    if not latest_job_id:
        return set()

    # construct url to specific test result
    job_result_path = f"/view/gs/test-platform-results/logs/{job_name}/{latest_job_id}"
    job_result_url = base_url + job_result_path

    # get junit xml urls
    junit_urls = fetch_junit_xml_urls(job_result_url, job_name, latest_job_id)

    if not junit_urls:
        print("no junit xml files found")
        return set()

    # parse all junit xml files and collect unique pod names
    all_pod_names = set()
    for url in junit_urls:
        pod_names = parse_junit_xml(url)
        all_pod_names.update(pod_names)

    # strip pod hashes to get base names
    deduplicated = set()
    for pod in all_pod_names:
        base_name = strip_pod_hash(pod)
        if base_name:
            deduplicated.add(base_name)

    return deduplicated


def main():
    # define jobs to scan
    jobs = {
        'tls_adherence': {
            'name': 'periodic-ci-openshift-tls-scanner-main-periodic-tls13-adherence',
            'path': '/job-history/gs/test-platform-results/logs/periodic-ci-openshift-tls-scanner-main-periodic-tls13-adherence',
            'description': 'tls 1.3 adherence (ocp 5.0 requirement)'
        },
        'pqc_readiness': {
            'name': 'periodic-ci-openshift-tls-scanner-main-periodic-pqc-readiness',
            'path': '/job-history/gs/test-platform-results/logs/periodic-ci-openshift-tls-scanner-main-periodic-pqc-readiness',
            'description': 'pqc readiness (ocp 4.22 requirement)'
        }
    }

    results = {}

    # fetch results for each job
    for job_key, job_info in jobs.items():
        print(f"\n{'='*60}")
        print(f"scanning: {job_info['description']}")
        print(f"{'='*60}")
        failed_pods = fetch_failed_pods(job_info['name'], job_info['path'])
        results[job_key] = failed_pods
        print(f"found {len(failed_pods)} failed pods")

    # combine results and categorize pods
    all_pods = set()
    for pods in results.values():
        all_pods.update(pods)

    # write combined output
    output_file = "tls_failure_report.txt"
    with open(output_file, 'w') as f:
        # header
        f.write("tls scanner results\n")
        f.write("=" * 60 + "\n\n")

        # pqc readiness failures (4.22 - comes first)
        f.write("pqc readiness failures (ocp 4.22 requirement):\n")
        f.write("-" * 60 + "\n")
        if results['pqc_readiness']:
            for pod in sorted(results['pqc_readiness']):
                f.write(f"{pod}\n")
        else:
            f.write("no failures\n")
        f.write("\n")

        # tls adherence failures (5.0 - comes second)
        f.write("tls 1.3 adherence failures (ocp 5.0 requirement):\n")
        f.write("-" * 60 + "\n")
        if results['tls_adherence']:
            for pod in sorted(results['tls_adherence']):
                f.write(f"{pod}\n")
        else:
            f.write("no failures\n")
        f.write("\n")

        # pods failing both tests
        both_failed = results['tls_adherence'] & results['pqc_readiness']
        if both_failed:
            f.write("pods failing both tests:\n")
            f.write("-" * 60 + "\n")
            for pod in sorted(both_failed):
                f.write(f"{pod}\n")
            f.write("\n")

        # summary
        f.write("summary:\n")
        f.write("-" * 60 + "\n")
        f.write(f"total unique pods with failures: {len(all_pods)}\n")
        f.write(f"pqc readiness failures (ocp 4.22): {len(results['pqc_readiness'])}\n")
        f.write(f"tls 1.3 adherence failures (ocp 5.0): {len(results['tls_adherence'])}\n")
        f.write(f"failing both tests: {len(both_failed)}\n")

    print(f"\n{'='*60}")
    print("summary:")
    print(f"{'='*60}")
    print(f"total unique pods with failures: {len(all_pods)}")
    print(f"pqc readiness failures (ocp 4.22): {len(results['pqc_readiness'])}")
    print(f"tls 1.3 adherence failures (ocp 5.0): {len(results['tls_adherence'])}")
    print(f"failing both tests: {len(both_failed)}")
    print(f"\nresults written to {output_file}")


if __name__ == "__main__":
    main()
