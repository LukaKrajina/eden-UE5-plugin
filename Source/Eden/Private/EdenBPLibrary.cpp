/*
    Eden for Unreal Engine
    Copyright (C) 2026 LukaKrajina

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU General Public License as published by
    the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU General Public License for more details.

    You should have received a copy of the GNU General Public License
    along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/
#include "EdenBPLibrary.h"
#include "Eden.h"
#include "HAL/PlatformProcess.h"

typedef void (*StartEngineFunc)(char*);
typedef void (*StopEngineFunc)();
typedef char* (*GetLocalPeerIDFunc)();
typedef char* (*EnterMatchmakingFunc)(char*, char*, int);
typedef void (*LeaveMatchmakingFunc)();
typedef void (*AdvertiseHostLobbyFunc)(char*, char*);
typedef char* (*FindMatchFunc)(char*, char*);
typedef double (*GetBalanceFunc)(char*);
typedef int (*SendEdenCoinFunc)(char*, double);
typedef char* (*PlaceBetFunc)(char*, char*, double);
typedef void (*FreeStringFunc)(char*);

void UEdenBPLibrary::StartEdenEngine(FString GameID)
{
	if (!FEdenModule::AdamDllHandle) return;
	StartEngineFunc StartEngine = (StartEngineFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("StartEngine"));
	if (StartEngine) StartEngine(TCHAR_TO_ANSI(*GameID));
}

void UEdenBPLibrary::StopEdenEngine()
{
	if (!FEdenModule::AdamDllHandle) return;
	StopEngineFunc StopEngine = (StopEngineFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("StopEngine"));
	if (StopEngine) StopEngine();
}

FString UEdenBPLibrary::GetLocalPeerID()
{
	if (!FEdenModule::AdamDllHandle) return TEXT("");
	GetLocalPeerIDFunc GetID = (GetLocalPeerIDFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("GetLocalPeerID"));
	FreeStringFunc FreeStr = (FreeStringFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("FreeString"));

	if (GetID && FreeStr)
	{
		char* Res = GetID();
		FString Ret(ANSI_TO_TCHAR(Res));
		FreeStr(Res); 
		return Ret;
	}
	return TEXT("");
}

FString UEdenBPLibrary::EnterMatchmaking(FString Mode, FString PartyList, int32 ExpectedPlayers)
{
	if (!FEdenModule::AdamDllHandle) return TEXT("Error: DLL Missing");
	EnterMatchmakingFunc Enter = (EnterMatchmakingFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("EnterMatchmaking"));
	FreeStringFunc FreeStr = (FreeStringFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("FreeString"));

	if (Enter && FreeStr)
	{
		char* Res = Enter(TCHAR_TO_ANSI(*Mode), TCHAR_TO_ANSI(*PartyList), ExpectedPlayers);
		FString Ret(ANSI_TO_TCHAR(Res));
		FreeStr(Res);
		return Ret;
	}
	return TEXT("Error: Method Failed");
}

void UEdenBPLibrary::LeaveMatchmaking()
{
	if (!FEdenModule::AdamDllHandle) return;
	LeaveMatchmakingFunc Leave = (LeaveMatchmakingFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("LeaveMatchmaking"));
	if (Leave) Leave();
}

void UEdenBPLibrary::AdvertiseHostLobby(FString Mode, FString MapName)
{
	if (!FEdenModule::AdamDllHandle) return;
	AdvertiseHostLobbyFunc Adv = (AdvertiseHostLobbyFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("AdvertiseHostLobby"));
	if (Adv) Adv(TCHAR_TO_ANSI(*Mode), TCHAR_TO_ANSI(*MapName));
}

FString UEdenBPLibrary::FindMatch(FString Mode, FString MapName)
{
	if (!FEdenModule::AdamDllHandle) return TEXT("Error: DLL Missing");
	FindMatchFunc Find = (FindMatchFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("FindMatch"));
	FreeStringFunc FreeStr = (FreeStringFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("FreeString"));

	if (Find && FreeStr)
	{
		char* Res = Find(TCHAR_TO_ANSI(*Mode), TCHAR_TO_ANSI(*MapName));
		FString Ret(ANSI_TO_TCHAR(Res));
		FreeStr(Res);
		return Ret;
	}
	return TEXT("Error: Method Failed");
}

float UEdenBPLibrary::GetBalance(FString Address)
{
	if (!FEdenModule::AdamDllHandle) return 0.0f;
	GetBalanceFunc Bal = (GetBalanceFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("GetBalance"));
	if (Bal) return (float)Bal(TCHAR_TO_ANSI(*Address));
	return 0.0f;
}

bool UEdenBPLibrary::SendEdenCoin(FString Receiver, float Amount)
{
	if (!FEdenModule::AdamDllHandle) return false;
	SendEdenCoinFunc Send = (SendEdenCoinFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("SendEdenCoin"));
	if (Send) return Send(TCHAR_TO_ANSI(*Receiver), (double)Amount) == 1;
	return false;
}

FString UEdenBPLibrary::PlaceBet(FString MatchID, FString Team, float Amount)
{
	if (!FEdenModule::AdamDllHandle) return TEXT("Error: DLL Missing");
	PlaceBetFunc Bet = (PlaceBetFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("PlaceBet"));
	FreeStringFunc FreeStr = (FreeStringFunc)FPlatformProcess::GetDllExport(FEdenModule::AdamDllHandle, TEXT("FreeString"));

	if (Bet && FreeStr)
	{
		char* Res = Bet(TCHAR_TO_ANSI(*MatchID), TCHAR_TO_ANSI(*Team), (double)Amount);
		FString Ret(ANSI_TO_TCHAR(Res));
		FreeStr(Res);
		return Ret;
	}
	return TEXT("Error: Method Failed");
}