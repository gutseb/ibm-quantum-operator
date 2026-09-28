"""Bell-state sanity test: verify local simulation actually works end to end before the operator's
"hello world" cluster test. Uses the exact qiskit/qiskit-aer versions this project has already pinned
and validated throughout (see container-images/profiles/pipeline-runner/requirements.txt) -- not
arbitrary versions.

A correct Bell state (|00> + |11>)/sqrt(2)) measures ~50% "00" and ~50% "11", and essentially 0% "01"/
"10" (the two qubits are perfectly correlated). This is the standard, minimal proof that entanglement
and measurement are both working correctly through this exact local stack -- not just that qiskit
imports cleanly.
"""
import sys

from qiskit import QuantumCircuit, transpile
from qiskit_aer import AerSimulator

SHOTS = 4096

def main() -> int:
    qc = QuantumCircuit(2, 2)
    qc.h(0)
    qc.cx(0, 1)
    qc.measure([0, 1], [0, 1])

    backend = AerSimulator()
    transpiled = transpile(qc, backend)
    result = backend.run(transpiled, shots=SHOTS).result()
    counts = result.get_counts()

    print(f"qiskit backend: {backend.name}")
    print(f"shots: {SHOTS}")
    print(f"counts: {counts}")

    correlated = counts.get("00", 0) + counts.get("11", 0)
    anticorrelated = counts.get("01", 0) + counts.get("10", 0)
    correlated_frac = correlated / SHOTS
    anticorrelated_frac = anticorrelated / SHOTS

    print(f"correlated (00+11) fraction: {correlated_frac:.4f}")
    print(f"anticorrelated (01+10) fraction: {anticorrelated_frac:.4f}")

    # Generous but meaningful thresholds -- true Bell state should be ~100% correlated with only
    # shot noise; anything above a few percent anticorrelated would indicate a real problem (wrong
    # circuit, broken CX, or a measurement/backend bug), not just statistical noise at 4096 shots.
    ok = correlated_frac > 0.98 and anticorrelated_frac < 0.02
    zero_count_ok = counts.get("00", 0) > 0 and counts.get("11", 0) > 0

    if ok and zero_count_ok:
        print("PASS: Bell state entanglement confirmed (both |00> and |11> observed, correlated >98%)")
        return 0
    else:
        print("FAIL: results are not consistent with a correct Bell state")
        return 1


if __name__ == "__main__":
    sys.exit(main())
