import json

with open("starknet_combined.json", "r") as ifp:
    abi_mapping = json.load(ifp)

union = []
for key, abi_items in abi_mapping.items():
    if isinstance(abi_items, list):
        union.extend(abi_items)
    else:
        union.append(abi_items)

with open("starknet_union.json", "w") as ofp:
    json.dump(union, ofp, indent=4)
