<?php
header('Content-Type: application/json');
header('Access-Control-Allow-Origin: *');

$file = __DIR__ . '/leaderboard.json';

$fp = fopen($file, file_exists($file) ? 'r+' : 'w+');
flock($fp, LOCK_EX);

$entries = [];
$raw = stream_get_contents($fp);
if ($raw) {
    $entries = json_decode($raw, true) ?: [];
}

if ($_SERVER['REQUEST_METHOD'] === 'POST') {
    $body = json_decode(file_get_contents('php://input'), true);
    $name = trim($body['name'] ?? '');
    $level = intval($body['level'] ?? 0);

    if ($name !== '' && $level > 0) {
        $name = strtoupper(substr($name, 0, 8));
        $entries[] = ['name' => $name, 'level' => $level];
        usort($entries, fn($a, $b) => $b['level'] - $a['level']);
        $entries = array_slice($entries, 0, 10);

        ftruncate($fp, 0);
        rewind($fp);
        fwrite($fp, json_encode($entries, JSON_PRETTY_PRINT));
    }
}

flock($fp, LOCK_UN);
fclose($fp);

echo json_encode($entries);
