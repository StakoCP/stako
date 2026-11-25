<?php
// SPDX-License-Identifier: MIT
// Official WHMCS Provisioning Module for Stako Control Panel (Phase 4 / v1.0)

if (!defined("WHMCS")) {
    die("This file cannot be accessed directly");
}

function stako_MetaData()
{
    return [
        'DisplayName' => 'Stako Control Panel',
        'APIVersion' => '1.1',
        'RequiresServer' => true,
        'DefaultNonSSLPort' => '8083',
        'DefaultSSLPort' => '8083',
        'ServiceSingleSignOnLabel' => 'Login to Stako Panel',
        'AdminSingleSignOnLabel' => 'Login to Stako Admin',
    ];
}

function stako_ConfigOptions()
{
    return [
        'Package ID' => ['Type' => 'text', 'Size' => '25', 'Default' => 'p_basic'],
        'Role' => ['Type' => 'dropdown', 'Options' => 'user,reseller', 'Default' => 'user'],
        'Shell' => ['Type' => 'dropdown', 'Options' => 'nologin,sftp,rbash,bash', 'Default' => 'nologin'],
    ];
}

function stako_api(array $params, string $method, string $path, ?array $body = null): array
{
    $host = $params['serverhostname'] ?: $params['serverip'];
    $port = $params['serverport'] ?: 8083;
    $scheme = $params['serversecure'] ? 'https' : 'http';
    $url = "{$scheme}://{$host}:{$port}/api/v1{$path}";

    $ch = curl_init($url);
    $headers = [
        'Accept: application/json',
        'Content-Type: application/json',
        'Authorization: Bearer ' . $params['serveraccesshash'],
    ];
    curl_setopt_array($ch, [
        CURLOPT_RETURNTRANSFER => true,
        CURLOPT_CUSTOMREQUEST => $method,
        CURLOPT_HTTPHEADER => $headers,
        CURLOPT_TIMEOUT => 20,
    ]);
    if ($body !== null) {
        curl_setopt($ch, CURLOPT_POSTFIELDS, json_encode($body));
    }
    $raw = curl_exec($ch);
    $code = curl_getinfo($ch, CURLINFO_HTTP_CODE);
    $err = curl_error($ch);
    curl_close($ch);

    if ($raw === false) {
        throw new Exception("Stako API connection error: " . $err);
    }
    $data = json_decode($raw, true) ?: [];
    if ($code >= 400) {
        $msg = $data['detail'] ?? $data['title'] ?? "HTTP {$code}";
        throw new Exception("Stako API error ({$code}): {$msg}");
    }
    return $data;
}

function stako_TestConnection(array $params)
{
    try {
        stako_api($params, 'GET', '/system/info');
        return ['success' => true, 'error' => ''];
    } catch (Exception $e) {
        return ['success' => false, 'error' => $e->getMessage()];
    }
}

function stako_CreateAccount(array $params)
{
    try {
        $res = stako_api($params, 'POST', '/users', [
            'username' => $params['username'],
            'email' => $params['clientsdetails']['email'],
            'password' => $params['password'],
            'role' => $params['configoption2'] ?: 'user',
            'package_id' => $params['configoption1'] ?: 'p_basic',
            'shell' => $params['configoption3'] ?: 'nologin',
        ]);
        if (!empty($params['domain'])) {
            stako_api($params, 'POST', '/domains', [
                'domain' => $params['domain'],
                'user_id' => $res['user']['id'],
            ]);
        }
        return 'success';
    } catch (Exception $e) {
        return $e->getMessage();
    }
}

function stako_findUserId(array $params): string
{
    $res = stako_api($params, 'GET', '/users?q=' . urlencode($params['username']));
    foreach ($res['items'] ?? [] as $u) {
        if ($u['username'] === $params['username']) {
            return $u['id'];
        }
    }
    throw new Exception('User not found in Stako: ' . $params['username']);
}

function stako_SuspendAccount(array $params)
{
    try {
        $id = stako_findUserId($params);
        stako_api($params, 'POST', "/users/{$id}/suspend");
        return 'success';
    } catch (Exception $e) {
        return $e->getMessage();
    }
}

function stako_UnsuspendAccount(array $params)
{
    try {
        $id = stako_findUserId($params);
        stako_api($params, 'POST', "/users/{$id}/unsuspend");
        return 'success';
    } catch (Exception $e) {
        return $e->getMessage();
    }
}

function stako_TerminateAccount(array $params)
{
    try {
        $id = stako_findUserId($params);
        stako_api($params, 'DELETE', "/users/{$id}");
        return 'success';
    } catch (Exception $e) {
        return $e->getMessage();
    }
}

function stako_ChangePackage(array $params)
{
    try {
        $id = stako_findUserId($params);
        stako_api($params, 'PATCH', "/users/{$id}", [
            'package_id' => $params['configoption1'] ?: 'p_basic',
        ]);
        return 'success';
    } catch (Exception $e) {
        return $e->getMessage();
    }
}

function stako_ChangePassword(array $params)
{
    try {
        $id = stako_findUserId($params);
        stako_api($params, 'PATCH', "/users/{$id}", [
            'password' => $params['password'],
        ]);
        return 'success';
    } catch (Exception $e) {
        return $e->getMessage();
    }
}

function stako_ServiceSingleSignOn(array $params)
{
    try {
        $id = stako_findUserId($params);
        $res = stako_api($params, 'POST', '/billing/sso', ['user_id' => $id]);
        $host = $params['serverhostname'] ?: $params['serverip'];
        $port = $params['serverport'] ?: 8083;
        $scheme = $params['serversecure'] ? 'https' : 'http';
        return ['success' => true, 'redirectTo' => "{$scheme}://{$host}:{$port}{$res['sso_url']}"];
    } catch (Exception $e) {
        return ['success' => false, 'errorMsg' => $e->getMessage()];
    }
}
