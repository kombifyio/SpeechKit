package io.kombify.speechkit.coinstall.v1;

import io.kombify.speechkit.coinstall.v1.ICoinstallCallback;
import io.kombify.speechkit.coinstall.v1.CapabilityStatus;
import io.kombify.speechkit.coinstall.v1.ProvisionRequest;
import io.kombify.speechkit.coinstall.v1.ProvisionResult;
import io.kombify.speechkit.coinstall.v1.TurnRequest;

interface ICoinstallService {
    int getContractVersion();
    CapabilityStatus getCapabilityStatus();
    ProvisionResult provision(in ProvisionRequest request);
    void startTurn(in TurnRequest request, in ICoinstallCallback callback);
    void cancelTurn(String turnId);
}
