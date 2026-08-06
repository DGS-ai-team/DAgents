from dagents_browser.ports import allocate_debug_port


def normalize_max_elements(n: int) -> int:
    # 与 driver 保持一致的轻量断言（避免 import browser_use）
    if n <= 0:
        return 150
    return min(n, 500)


def test_normalize_max_elements():
    assert normalize_max_elements(0) == 150
    assert normalize_max_elements(200) == 200
    assert normalize_max_elements(999) == 500


def test_allocate_debug_port_unique():
    p1 = allocate_debug_port("agt-a-browser", base_port=9222)
    p2 = allocate_debug_port("agt-b-browser", base_port=9222, used_ports={p1})
    assert p1 != p2
    assert 9222 <= p1 < 9222 + 200
    assert 9222 <= p2 < 9222 + 200


def test_allocate_debug_port_attach_mode():
    assert allocate_debug_port("any", base_port=9333, cdp_url="http://127.0.0.1:9333") == 9333
